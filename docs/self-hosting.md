# Self-hosting a model for yoli

Yoli talks to any OpenAI-compatible `/chat/completions` endpoint, so you
can run your own model on a GPU box and point yoli at it with a provider
profile in `~/.config/yoli/config.json`:

```json
{
  "default_provider": "selfhosted",
  "providers": {
    "selfhosted": {
      "base_url": "https://<your-endpoint>/v1",
      "api_key": "<your-secret>",
      "model": "<exact served model name>"
    }
  }
}
```

This guide walks through [RunPod](https://www.runpod.io) — cloud GPU
pods rented by the hour — with [vLLM](https://docs.vllm.ai) as the
serving engine. Any host that gives you a GPU and a TLS endpoint works
the same way.

## Requirement: the model must support tool calling

Yoli's agent loop drives everything through OpenAI-style function
calling. A backend that never returns `tool_calls` produces an agent
that answers once and stops. Two consequences:

1. Pick a model trained for tool use — e.g. `Qwen/Qwen2.5-Coder-32B-Instruct`,
   `meta-llama/Llama-3.3-70B-Instruct`, or a quantized (AWQ/GPTQ)
   variant sized to your GPU.
2. Start vLLM with tool-call parsing enabled:
   `--enable-auto-tool-choice --tool-call-parser <parser>`, where the
   parser matches the model family (`hermes` for Qwen, `llama3_json`
   for Llama 3.x, `mistral` for Mistral).

## Hosting on a RunPod GPU pod

1. **Create a pod.** In the RunPod console, deploy a GPU pod using the
   official `vllm/vllm-openai:latest` image. Size the GPU to the model:
   a 24 GB card (RTX 4090 / A5000) runs 7–14B models or a 32B AWQ
   quant; a 80 GB A100/H100 runs 32B+ at full precision.

2. **Set the container command.** vLLM serves an OpenAI-compatible API
   on port 8000:

   ```
   --model Qwen/Qwen2.5-Coder-32B-Instruct-AWQ
   --api-key <long-random-secret>
   --enable-auto-tool-choice
   --tool-call-parser hermes
   ```

   Generate the secret yourself (e.g. `openssl rand -hex 32`). For
   gated Hugging Face models, add your `HF_TOKEN` as a RunPod secret.
   Attach a volume at `/root/.cache/huggingface` so restarts don't
   re-download the weights.

3. **Expose port 8000 as an HTTP port.** RunPod's proxy terminates TLS
   for you at:

   ```
   https://<pod-id>-8000.proxy.runpod.net
   ```

   The base URL for yoli is that address plus `/v1`.

4. **Point yoli at the pod** with a provider profile in
   `~/.config/yoli/config.json`:

   ```json
   {
     "default_provider": "runpod",
     "providers": {
       "runpod": {
         "base_url": "https://<pod-id>-8000.proxy.runpod.net/v1",
         "api_key": "<the --api-key secret>",
         "model": "Qwen/Qwen2.5-Coder-32B-Instruct-AWQ"
       }
     }
   }
   ```

   then `yoli chat "hi"`.

   The model name must match what vLLM serves exactly — yoli passes it
   through verbatim. Check with:

   ```bash
   curl -H "Authorization: Bearer <secret>" \
     https://<pod-id>-8000.proxy.runpod.net/v1/models
   ```

### Serverless alternative

RunPod's serverless vLLM endpoints scale to zero and bill per request.
They expose the same OpenAI-compatible API at:

```
https://api.runpod.ai/v2/<endpoint-id>/openai/v1
```

with your RunPod API key as the bearer token — set that URL as
`base_url` and the RunPod API key as `api_key`. Expect cold-start
latency on the first request after idle.

## Security notes

- **The proxy URL is publicly reachable.** Anyone who finds it can call
  your GPU. vLLM's `--api-key` flag is therefore non-optional: without
  it the endpoint is an open relay billed to your account.
- **Treat the key like any credential.** Store it in the provider
  profile's `api_key` field in `~/.config/yoli/config.json` rather than
  exporting it in shell history or committing it. Yoli
  sends it only as an `Authorization: Bearer` header over TLS.
- **Don't expose the raw TCP port.** RunPod can also expose ports
  without the TLS proxy; that path ships your prompts and key in
  plaintext. Stick to the `https://…proxy.runpod.net` address.
- **Stop the pod when done.** Idle pods bill by the hour and remain
  reachable; serverless endpoints scale to zero on their own.

See also [providers.md](providers.md) and
[configuration.md](configuration.md).
