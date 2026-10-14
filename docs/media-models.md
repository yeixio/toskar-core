# Image and video models

Toskar makes pictures and clips on your own computers with [stable-diffusion.cpp](https://github.com/leejet/stable-diffusion.cpp). The prompt, the pictures, and the clips aren't sent anywhere. This page lists the models it can set up, what each needs, and what to expect. For the tools themselves, see [Images](tools.md#images) and [Video](tools.md#video).

Better hardware makes a model faster, or lets a larger model run. It doesn't change what a given model can draw.

## Quality

Each model has a tier: **fast**, **balanced**, or **best**. Asking for "a high-quality picture" or "a quick draft" uses the set-up model of that tier, and the image and video tools take `quality` (`fast`, `balanced`, or `best`). When that model isn't set up, the current one makes it, and the answer names the model that would give the quality asked for. A profile can set the quality used when a request doesn't ask for one (**Profiles & Orchestration → Behavior → Picture and clip quality**), and each answer's steps say which model made the picture or clip. Edits and clips from a picture use the model that can make them, whatever the quality.

| Tier | Pictures | Clips |
| --- | --- | --- |
| Fast | FLUX.2 [klein] 4B | Wan 2.1 T2V 1.3B |
| Balanced | FLUX.2 [klein] 4B, high quality | Wan 2.2 TI2V 5B |
| Best | Z-Image-Turbo | Wan 2.2 T2V A14B |

## Pictures

| Model | Does | Download | Comfortable with | Tested |
| --- | --- | --- | --- | --- |
| FLUX.2 [klein] 4B | Makes and edits | 5.2 GB | 12 GB of memory | Yes: RX 7900 XTX, about 10–22 s a picture, peak 6–9 GB of graphics memory |
| FLUX.2 [klein] 4B, high quality | Makes and edits, with more detail | 8.8 GB | 20 GB | Not yet |
| Z-Image-Turbo | Makes photorealistic pictures, with clear text in them | 6.7 GB | 14 GB (estimate) | Yes: RX 7900 XTX, about 35–40 s a picture |
| Qwen-Image-Edit-2511 | Edits only: changes what's asked, keeps the rest, edits text | 19.5 GB | 32 GB | Yes: RX 7900 XTX, about 80 s an edit, peak 24 GB of memory and 14 GB of graphics memory |

- **Recommended:** the model Toskar picks for you does everything, making and editing. That's one of the FLUX.2 [klein] 4B builds, by memory. The others are set up by choice on the Tools page.
- **Several at once:** with Z-Image-Turbo as your picture model, edits use FLUX.2 [klein] when it's set up. With Qwen-Image-Edit-2511 set up, edits use it, and new pictures use your picture model.
- **Size:** pictures are up to 1536 pixels on a side and about 1.5 megapixels. Asking for more makes the largest picture the model allows, and the answer says so.

## Needs your license

Some models can't be given out by Toskar: their license is non-commercial, or allows commercial use only under the vendor's terms. Toskar sets one up only when you bring your own license:

1. Accept the model's license on Hugging Face with your account.
2. Add a Hugging Face read token on the Tools page, or as `huggingface_token` in Settings. It's kept on this computer, never shown again, and sent only to Hugging Face, for that model's files.
3. Confirm that your license covers how you'll use the model. Toskar can't check a license; it keeps your confirmation and when you gave it.

The files come straight from the vendor's own repository, never a mirror. These models are never recommended or picked by quality.

| Model | Does | Download | Comfortable with | License |
| --- | --- | --- | --- | --- |
| FLUX.2 [klein] 9B | Makes and edits, with more detail than the 4B | 23.4 GB | 32 GB (tested: about 36 s a picture, peak 26 GB of memory and 20 GB of graphics memory, on an RX 7900 XTX) | [FLUX Non-Commercial](https://huggingface.co/black-forest-labs/FLUX.2-klein-9B): non-commercial use, or a commercial license from Black Forest Labs |

## Clips

| Model | Does | Download | Comfortable with | Tested |
| --- | --- | --- | --- | --- |
| Wan 2.2 TI2V 5B | Clips from a description or a picture | 8.5 GB | 16 GB | Yes: about 1 minute for 2 seconds on an RX 7900 XTX (peak 5 GB of graphics memory), 3½ minutes on an M5 Pro with 24 GB |
| Wan 2.2 T2V A14B | Higher-quality clips from a description, much more slowly | 23 GB | 48 GB | Yes: RX 7900 XTX, about 13 minutes for 2 seconds, peak 46 GB of memory |
| Wan 2.1 T2V 1.3B | Short clips from a description, on smaller computers | 6.8 GB | 10 GB (estimate) | Yes: RX 7900 XTX, about 3 minutes for 2 seconds, peak 11 GB of graphics memory |

- **Recommended:** Wan 2.2 TI2V 5B, which also brings pictures to life. The A14B and 1.3B models take no picture, so clips from a picture use the 5B model when it's set up.
- **Length and size:** clips are 2 seconds unless you ask for more, up to 5 seconds, at up to about 832×480. Asking for longer or larger makes the most the model allows, and the answer says so.

"Comfortable with" is the memory a model runs well with: system memory, or unified memory on Apple silicon. Each model also has the graphics memory it works best with. On a computer with its own graphics card, that's the card's memory; with less, parts of it run from system memory and it's slower. On Apple silicon, the graphics may use about two thirds of the computer's memory, or three quarters above 36 GB, and a model needing more than that may be slow or fail.

Each model gets a verdict for the computer it would run on, on the Tools page and in a chat's setup offer:

- **Works well:** it fits, with room to spare.
- **May be slow or tight:** it fits, but below the memory or graphics memory it's comfortable with. Toskar says what it needs and what the computer has, such as "Needs about 16 GB of graphics memory; this computer's graphics card has 10 GB". It's set up only when you agree.
- **Not supported here:** it can't fit at all. A lighter model, a smaller or shorter request, or a paired computer with more memory can make it instead. An estimate hasn't been measured on real hardware yet; "Not yet" models can be set up, but they're labeled as untested and never recommended.

## Test machines

| Computer | Status |
| --- | --- |
| Linux, AMD Radeon RX 7900 XTX 24 GB, Vulkan | Tested in the [media quality set](../tests/quality/README.md) |
| Mac, Apple silicon (M5 Pro), 24 GB unified memory, Metal | Wan 2.2 TI2V 5B measured; the others not yet |
| Windows or Linux, NVIDIA RTX 3080 10–12 GB, Vulkan | Not yet measured |

24 GB of unified memory isn't 24 GB of graphics memory: the system and other apps share it, so a model comfortable with 24 GB may be tight on a 24 GB Mac.

## When memory is short

- **Before a picture or clip:** Toskar unloads the chat models in its way, waiting for an answer being written to finish, and holds new model loads until the picture or clip is done.
- **If it still runs out:** the answer says so plainly and suggests a smaller picture, a shorter or smaller clip, or a [paired computer](clustering.md#tools-on-other-computers) with more memory.
- **A paired computer:** one with image or video generation set up can make them for this one. Toskar prefers one whose GPU does the work.

## Troubleshooting

- **Slow pictures or clips:** check the Tools page. It says whether the GPU build or the CPU build makes them. On Linux and Windows the GPU build needs a Vulkan driver; see [GPU acceleration](gpu.md). Without one, a picture takes minutes and a clip can take most of an hour.
- **"Ran out of memory":** close other large apps, ask for a smaller picture or shorter clip, or set up a lighter model.
- **"The graphics card stopped responding":** try again. If it keeps happening, restart the computer or update the graphics driver.
- **Mac App Store build:** image and video generation aren't available, since the App Sandbox can't run a program Toskar downloads. Use a copy of Toskar from outside the Mac App Store, or a paired computer.

## Where the list comes from

The models are listed in `manifests/models/media.json`, built into Toskar. A `media.json` in the data folder's `manifests/models` replaces it, as `catalog.json` does for chat models. Every file in it must be pinned, with its size, SHA-256, and a revision or full address; a file that isn't is refused, and the built-in list stays.

## Licenses and sources

Every model and part Toskar sets up is under Apache 2.0, except TAEHV's fast video decoders, which are MIT. None needs an account to download. Each file is pinned to a revision and checked against its SHA-256.

| Model | Source |
| --- | --- |
| FLUX.2 [klein] 4B | [black-forest-labs/FLUX.2-klein-4B](https://huggingface.co/black-forest-labs/FLUX.2-klein-4B) |
| Z-Image-Turbo | [Tongyi-MAI/Z-Image-Turbo](https://huggingface.co/Tongyi-MAI/Z-Image-Turbo) |
| Qwen-Image-Edit-2511 | [Qwen/Qwen-Image-Edit-2511](https://huggingface.co/Qwen/Qwen-Image-Edit-2511) |
| Wan 2.2 | [Wan-Video/Wan2.2](https://github.com/Wan-Video/Wan2.2) |
| Wan 2.1 | [Wan-Video/Wan2.1](https://github.com/Wan-Video/Wan2.1) |

Models under non-commercial or community licenses are set up only with your own license (see [Needs your license](#needs-your-license)). FLUX.2 Dev isn't offered: its official files are 64 GB with a 24B text encoder.
