"""Yggdrasil PyTorch PEFT LoRA trainer.

The daemon runs this script inside its managed Python environment with a
JSON config path as the only argument. Progress lines start with "@@ygg " and
carry one JSON object, the same protocol as mlx_train.py. Everything else is
log output.

It trains on an NVIDIA GPU with CUDA. QLoRA loads the base weights in 4-bit
with bitsandbytes, which needs CUDA. On a computer without CUDA it falls back
to Apple's MPS or the CPU, which is slow and meant for testing.

Stages: loading_model (download and load weights), training, exporting (write
the adapter as a GGUF LoRA that llama-server loads next to the base GGUF).
"""

import json
import math
import os
import random
import sys
import threading
import time


def emit(**kw):
    print("@@ygg " + json.dumps(kw), flush=True)


def dir_bytes(path):
    total = 0
    for root, _, files in os.walk(path):
        for f in files:
            path = os.path.join(root, f)
            # Snapshot entries are symlinks to blobs; count each file once.
            if os.path.islink(path):
                continue
            try:
                total += os.path.getsize(path)
            except OSError:
                pass
    return total


def download(repo):
    from huggingface_hub import HfApi, snapshot_download

    patterns = ["*.json", "*.safetensors", "*.model", "*.txt", "*.tiktoken", "*.jinja"]
    total = 0
    try:
        info = HfApi().model_info(repo, files_metadata=True)
        total = sum((s.size or 0) for s in info.siblings if s.rfilename.endswith(".safetensors"))
    except Exception as exc:  # offline with a warm cache still works
        print(f"could not read repository size: {exc}", flush=True)

    hub = os.path.join(os.environ.get("HF_HOME", os.path.expanduser("~/.cache/huggingface")), "hub")
    folder = os.path.join(hub, "models--" + repo.replace("/", "--"))
    done = threading.Event()

    def watch():
        while not done.wait(1.0):
            emit(event="download", bytes=dir_bytes(folder), total=total)

    t = threading.Thread(target=watch, daemon=True)
    t.start()
    try:
        path = snapshot_download(repo, allow_patterns=patterns)
    finally:
        done.set()
        t.join()
    emit(event="download", bytes=dir_bytes(folder), total=total)
    return path


def pick_device(torch, wanted):
    if wanted not in ("", "auto"):
        return wanted
    if torch.cuda.is_available():
        return "cuda"
    if getattr(torch.backends, "mps", None) and torch.backends.mps.is_available():
        return "mps"
    return "cpu"


def peak_memory_gb(torch, device):
    if device == "cuda":
        return torch.cuda.max_memory_allocated() / 1e9
    if device == "mps":
        return torch.mps.driver_allocated_memory() / 1e9
    return None


def load_examples(path, tokenizer, max_len):
    """Tokenize chat examples. Only the last assistant turn is trained on:
    the prompt's labels are -100, as mlx-lm does with mask_prompt."""
    out = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            messages = json.loads(line)["messages"]
            if len(messages) < 2 or messages[-1]["role"] != "assistant":
                continue
            prompt = tokenizer.apply_chat_template(messages[:-1], tokenize=False, add_generation_prompt=True)
            full = tokenizer.apply_chat_template(messages, tokenize=False)
            prompt_ids = tokenizer(prompt, add_special_tokens=False)["input_ids"]
            ids = tokenizer(full, add_special_tokens=False)["input_ids"][:max_len]
            labels = ([-100] * len(prompt_ids) + ids[len(prompt_ids):])[: len(ids)]
            if any(l != -100 for l in labels):
                out.append((ids, labels))
    return out


def batches(examples, size, pad_id, torch, device, rng):
    """Yield padded batches forever, reshuffling each pass."""
    order = list(range(len(examples)))
    while True:
        rng.shuffle(order)
        for i in range(0, len(order), size):
            chunk = [examples[j] for j in order[i : i + size]]
            width = max(len(ids) for ids, _ in chunk)
            ids = [x + [pad_id] * (width - len(x)) for x, _ in chunk]
            labels = [y + [-100] * (width - len(y)) for _, y in chunk]
            mask = [[1] * len(x) + [0] * (width - len(x)) for x, _ in chunk]
            yield (
                torch.tensor(ids, device=device),
                torch.tensor(labels, device=device),
                torch.tensor(mask, device=device),
                sum(len(x) for x, _ in chunk),
            )


def export_gguf(state, model_dir, architecture, alpha, out_path):
    """Write a PEFT LoRA as a GGUF LoRA for llama.cpp.

    PEFT already uses llama.cpp's layout, lora_a (r, in) and lora_b (out, r),
    scaled by alpha / r. Llama-family GGUFs permute q and k rows for rotary
    embeddings; the rows of lora_b get the same permutation.
    """
    import gguf
    import numpy as np

    with open(os.path.join(model_dir, "config.json")) as f:
        hf = json.load(f)
    arch = {"qwen2": gguf.MODEL_ARCH.QWEN2, "llama": gguf.MODEL_ARCH.LLAMA}[architecture]
    tmap = gguf.TensorNameMap(arch, hf["num_hidden_layers"])

    def permute(t, n_head):
        return t.reshape(n_head, 2, t.shape[0] // n_head // 2, *t.shape[1:]).swapaxes(1, 2).reshape(t.shape)

    tmp = out_path + ".tmp"
    writer = gguf.GGUFWriter(tmp, gguf.MODEL_ARCH_NAMES[arch])
    writer.add_type(gguf.GGUFType.ADAPTER)
    writer.add_string(gguf.Keys.Adapter.TYPE, "lora")
    writer.add_float32(gguf.Keys.Adapter.LORA_ALPHA, float(alpha))
    count = 0
    for key in sorted(state):
        if ".lora_A." not in key:
            continue
        # base_model.model.model.layers.3.self_attn.q_proj.lora_A.weight
        stem = key.split(".lora_A.")[0].removeprefix("base_model.model.")
        name = tmap.get_name(stem + ".weight", try_suffixes=(".weight",))
        if name is None:
            raise ValueError(f"no GGUF tensor name for {stem}")
        a = state[key].float().cpu().numpy()
        b = state[key.replace(".lora_A.", ".lora_B.")].float().cpu().numpy()
        if architecture == "llama":
            if stem.endswith("q_proj"):
                b = permute(b, hf["num_attention_heads"])
            elif stem.endswith("k_proj"):
                b = permute(b, hf.get("num_key_value_heads", hf["num_attention_heads"]))
        writer.add_tensor(name + ".lora_a", np.ascontiguousarray(a))
        writer.add_tensor(name + ".lora_b", np.ascontiguousarray(b))
        count += 1
    writer.write_header_to_file()
    writer.write_kv_data_to_file()
    writer.write_tensors_to_file()
    writer.close()
    os.replace(tmp, out_path)
    return count


def main():
    with open(sys.argv[1]) as f:
        cfg = json.load(f)
    h = cfg["hyper"]

    emit(event="stage", stage="loading_model", detail="Downloading base weights")
    model_dir = download(cfg["repo"])

    emit(event="stage", stage="loading_model", detail="Loading the model")
    import torch
    from peft import LoraConfig, get_peft_model
    from transformers import AutoModelForCausalLM, AutoTokenizer

    device = pick_device(torch, cfg.get("device", "auto"))
    print(f"training on {device}", flush=True)
    seed = h.get("seed", 0)
    torch.manual_seed(seed)
    rng = random.Random(seed)

    tokenizer = AutoTokenizer.from_pretrained(model_dir)
    if tokenizer.pad_token_id is None:
        tokenizer.pad_token = tokenizer.eos_token
    qlora = h.get("method") == "qlora"
    kwargs = {}
    if qlora:
        if device != "cuda":
            raise RuntimeError("QLoRA needs an NVIDIA GPU with CUDA; train with LoRA instead")
        from transformers import BitsAndBytesConfig

        compute = torch.bfloat16 if torch.cuda.is_bf16_supported() else torch.float16
        kwargs["quantization_config"] = BitsAndBytesConfig(
            load_in_4bit=True, bnb_4bit_quant_type="nf4", bnb_4bit_compute_dtype=compute, bnb_4bit_use_double_quant=True
        )
        kwargs["device_map"] = {"": 0}
    elif device == "cuda":
        kwargs["dtype"] = torch.bfloat16 if torch.cuda.is_bf16_supported() else torch.float16
    else:
        # MPS and CPU train in full precision; half precision is unstable there.
        kwargs["dtype"] = torch.float32
    model = AutoModelForCausalLM.from_pretrained(model_dir, **kwargs)
    if not qlora:
        model.to(device)
    if qlora:
        from peft import prepare_model_for_kbit_training

        model = prepare_model_for_kbit_training(model, use_gradient_checkpointing=bool(h.get("grad_checkpoint")))
    elif h.get("grad_checkpoint"):
        model.gradient_checkpointing_enable()
        model.enable_input_require_grads()
    model.config.use_cache = False

    total_layers = model.config.num_hidden_layers
    layers = h.get("layers") or total_layers
    if layers < 0 or layers > total_layers:
        layers = total_layers
    rank = h["rank"]
    alpha = h["scale"] * rank
    lora = LoraConfig(
        r=rank,
        lora_alpha=alpha,
        lora_dropout=0.0,
        bias="none",
        # The same projections mlx-lm trains, on the last `layers` layers.
        target_modules=["q_proj", "v_proj"],
        layers_to_transform=list(range(total_layers - layers, total_layers)),
        task_type="CAUSAL_LM",
    )
    model = get_peft_model(model, lora)

    max_len = h["max_seq_length"]
    train = load_examples(os.path.join(cfg["data_dir"], "train.jsonl"), tokenizer, max_len)
    valid = load_examples(os.path.join(cfg["data_dir"], "valid.jsonl"), tokenizer, max_len)
    if not train:
        raise RuntimeError("no training examples could be read")
    batch_size = max(1, h["batch_size"])
    iters = max(1, h["iters"])
    optimizer = torch.optim.AdamW([p for p in model.parameters() if p.requires_grad], lr=h["learning_rate"])
    stream = batches(train, batch_size, tokenizer.pad_token_id, torch, device, rng)
    report_every = max(1, iters // 50)
    eval_every = max(5, iters // 5)

    def val_loss():
        if not valid:
            return None
        model.eval()
        losses = []
        with torch.no_grad():
            for ids, labels, mask, _ in batches(valid, batch_size, tokenizer.pad_token_id, torch, device, random.Random(0)):
                losses.append(model(input_ids=ids, attention_mask=mask, labels=labels).loss.item())
                if len(losses) >= min(25, math.ceil(len(valid) / batch_size)):
                    break
        model.train()
        return sum(losses) / len(losses)

    emit(event="stage", stage="training", detail="Training")
    model.train()
    started = time.time()
    window_loss, window_tokens, window_start, window_steps = 0.0, 0, time.time(), 0
    last_train, last_val = None, None
    for it in range(1, iters + 1):
        ids, labels, mask, tokens = next(stream)
        loss = model(input_ids=ids, attention_mask=mask, labels=labels).loss
        loss.backward()
        optimizer.step()
        optimizer.zero_grad(set_to_none=True)
        window_loss += loss.item()
        window_tokens += tokens
        window_steps += 1
        if it % report_every == 0 or it == iters:
            elapsed = max(time.time() - window_start, 1e-6)
            last_train = window_loss / window_steps
            emit(
                event="progress",
                iter=it,
                iters=iters,
                train_loss=last_train,
                tokens_per_sec=window_tokens / elapsed,
                it_per_sec=window_steps / elapsed,
                peak_memory_gb=peak_memory_gb(torch, device),
            )
            window_loss, window_tokens, window_start, window_steps = 0.0, 0, time.time(), 0
        if it % eval_every == 0 or it == iters:
            last_val = val_loss()
            if last_val is not None:
                emit(event="val", iter=it, val_loss=last_val)

    emit(event="stage", stage="exporting", detail="Writing the adapter")
    os.makedirs(cfg["adapter_dir"], exist_ok=True)
    model.save_pretrained(cfg["adapter_dir"])
    from peft import get_peft_model_state_dict

    state = get_peft_model_state_dict(model)
    tensors = export_gguf(state, model_dir, cfg["architecture"], alpha, cfg["adapter_out"])
    emit(
        event="done",
        adapter=cfg["adapter_out"],
        tensors=tensors,
        train_loss=last_train,
        val_loss=last_val,
        seconds=round(time.time() - started, 1),
    )


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:  # the daemon shows this message to the user
        emit(event="error", message=f"{type(exc).__name__}: {exc}")
        raise
