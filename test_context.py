#!/usr/bin/env python3
"""Test context retention across all configured providers via API-Switch proxy."""
import json
import subprocess
import sys
import time
import re
import urllib.request
import urllib.error

PROXY_PORT = 9030
PROXY_URL = f"http://localhost:{PROXY_PORT}/v1/messages"
TIMEOUT = 60

RESULTS = []

def stream_request(model, messages, max_tokens=0):
    """Send a streaming Anthropic-format request through the proxy."""
    body = json.dumps({
        "model": model,
        "messages": messages,
        "max_tokens": max_tokens,
        "stream": True
    }).encode()

    req = urllib.request.Request(PROXY_URL, data=body,
        headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
            full_text = ""
            for line_bytes in resp:
                line = line_bytes.decode("utf-8", errors="replace")
                if line.startswith("data: "):
                    data_str = line[6:].strip()
                    if data_str == "[DONE]":
                        break
                    try:
                        chunk = json.loads(data_str)
                        if "delta" in chunk and "text" in chunk.get("delta", {}):
                            full_text += chunk["delta"]["text"]
                    except json.JSONDecodeError:
                        pass
            return full_text.strip()
    except urllib.error.HTTPError as e:
        err_body = e.read().decode(errors="replace")
        return f"HTTP {e.code}: {err_body[:200]}"
    except Exception as e:
        return f"Error: {str(e)[:200]}"


def test_context(model, provider_name):
    """Test context retention: set context, then recall it."""
    name = "Bob"
    color = "green"

    # Turn 1: Set context
    r1 = stream_request(model, [
        {"role": "user", "content": f"My name is {name} and my favorite color is {color}. Reply 'OK' only."}
    ])
    r1_summary = r1[:80].replace("\n", " ")

    # Turn 2: Recall name
    r2 = stream_request(model, [
        {"role": "user", "content": f"My name is {name} and my favorite color is {color}. Reply 'OK' only."},
        {"role": "assistant", "content": r1[:200]},
        {"role": "user", "content": "What is my name?"}
    ])
    name_match = name.lower() in r2.lower() if r2 else False
    r2_summary = r2[:80].replace("\n", " ")

    # Turn 3: Recall color
    r3 = stream_request(model, [
        {"role": "user", "content": f"My name is {name} and my favorite color is {color}. Reply 'OK' only."},
        {"role": "assistant", "content": r1[:200]},
        {"role": "user", "content": "What is my name?"},
        {"role": "assistant", "content": r2[:200]},
        {"role": "user", "content": "What is my favorite color?"}
    ])
    color_match = color.lower() in r3.lower() if r3 else False
    r3_summary = r3[:80].replace("\n", " ")

    return {
        "model": model,
        "provider": provider_name,
        "r1": r1_summary,
        "r2": r2_summary,
        "r3": r3_summary,
        "name_ok": name_match,
        "color_ok": color_match,
        "passed": name_match and color_match
    }


def get_configured_models():
    """Parse api-switch.yaml to find all configured models with real API keys."""
    config_path = subprocess.run(
        "echo ~/.api-switch.yaml", shell=True, capture_output=True, text=True
    ).stdout.strip()
    config_path = subprocess.run(
        f"realpath {config_path}", shell=True, capture_output=True, text=True
    ).stdout.strip()

    with open(config_path) as f:
        config = f.read()

    # Find models and their providers
    models = re.findall(r'(\S+):\s*\n\s+provider:\s+(\S+)', config)
    # Find providers with non-placeholder API keys
    providers = {}
    provider_blocks = re.findall(r'(\S+):\s*\n\s+type:\s+(\S+)\s*\n\s+api_key:\s+(\S+)', config)
    for name, ptype, key in provider_blocks:
        if not key.startswith("<") and not key.startswith("sk-test") and not key.startswith("sk-ant-test"):
            providers[name] = {"type": ptype, "key": key}

    result = []
    for model, prov in models:
        if prov in providers:
            result.append((model, prov))
    return result


def print_header():
    print()
    print("=" * 100)
    print("  API-Switch 上下文保持能力测试 — 所有已配置大模型")
    print("=" * 100)
    print()


def print_summary():
    print()
    print("=" * 100)
    total = len(RESULTS)
    passed = sum(1 for r in RESULTS if r["passed"])
    print(f"  总计: {total}  通过: {passed}  失败: {total - passed}")
    print("=" * 100)
    print()
    if total - passed > 0:
        print("失败列表:")
        for r in RESULTS:
            if not r["passed"]:
                print(f"  ✗ {r['model']} ({r['provider']}): name={r['name_ok']} color={r['color_ok']}")


def main():
    # Start proxy
    print("Starting API-Switch proxy...")
    subprocess.run("ps aux | awk '/api-switch serve/ && !/awk/ {print $2}' | xargs -r kill 2>/dev/null", shell=True)
    time.sleep(1)
    subprocess.Popen(
        f"cd /root/API-Switch && ./api-switch serve -p {PROXY_PORT} > /dev/null 2>&1",
        shell=True
    )
    time.sleep(2)

    # Wait for proxy
    for _ in range(5):
        try:
            urllib.request.urlopen(f"http://localhost:{PROXY_PORT}/health", timeout=3)
            break
        except:
            time.sleep(1)
    else:
        print("ERROR: Proxy did not start")
        sys.exit(1)

    # Get configured models
    models = get_configured_models()
    if not models:
        print("ERROR: No configured models found with real API keys")
        sys.exit(1)

    print(f"Found {len(models)} configured models with real API keys")
    print()
    print(f"{'MODEL':<30} {'PROVIDER':<15} {'NAME':<8} {'COLOR':<8} {'R1':<30} {'R2':<30}")
    print("-" * 100)

    for model, provider in models:
        print(f"{model:<30} {provider:<15} {'...':<8} {'...':<8} {'testing...':<30}", end="", flush=True)
        result = test_context(model, provider)
        RESULTS.append(result)
        # Overwrite the line
        name_icon = "✓" if result["name_ok"] else "✗"
        color_icon = "✓" if result["color_ok"] else "✗"
        passed_icon = "PASS" if result["passed"] else "FAIL"
        print(f"\r{model:<30} {provider:<15} {name_icon:<8} {color_icon:<8} {result['r1'][:28]:<30} {result['r2'][:28]:<30} {passed_icon}")

    print_summary()


if __name__ == "__main__":
    main()
