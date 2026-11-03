# Toskar Grid — Distributed Single-Model Inference Research

## Research Status

**Status:** Research / post-MVP  
**Commitment:** None  
**Primary question:** Can Toskar evolve its current multi-computer team functionality into a grid that allows one model instance to run across multiple physical computers?

This document is intentionally a research brief rather than a feature specification. It records the opportunity, current technical approaches, risks, experiments, and architectural questions that should be answered before Toskar commits to implementation.

---

## 1. Vision

Today, Toskar teams can place complete models and jobs on different computers.

The long-term idea is different:

> Treat multiple computers as one logical inference resource so that a model that cannot fit on any individual machine may still run across the team.

The desired user experience is analogous to a Kubernetes cluster:

```text
Physical computers
┌──────────────┐
│ MacBook      │  24 GB
├──────────────┤
│ Linux PC     │  24 GB GPU
├──────────────┤
│ Mac Mini     │  16 GB
└──────────────┘
        │
        ▼
┌─────────────────────────────┐
│       Toskar Grid        │
│                             │
│  Logical distributed model │
│  execution environment      │
└─────────────────────────────┘
        │
        ▼
┌─────────────────────────────┐
│ One model instance spanning │
│ multiple physical machines  │
└─────────────────────────────┘
```

A user should eventually be able to see something like:

```text
Llama 70B
Doesn't fit on any single computer
Fits on team: 3 computers
Estimated performance: Moderate

[Run on team]
```

The long-term abstraction should feel like:

> "I have 64 GB of usable AI capacity across my team."

rather than:

> "I need to manually configure pipeline rank 0, tensor rank 1, RPC endpoints, and shard percentages."

---

## 2. Important Distinction: Team Routing vs Grid Execution

### Current team model

Toskar currently treats each model execution as belonging to one machine.

```text
Request A -> Computer 1 -> Model A
Request B -> Computer 2 -> Model B
Request C -> Computer 3 -> Model C
```

This is workload routing.

### Proposed grid model

A single request and a single model are executed by multiple machines together.

```text
Request
   │
   ▼
Model
   │
   ├── Layers / tensors -> Computer 1
   ├── Layers / tensors -> Computer 2
   └── Layers / tensors -> Computer 3
```

This is distributed inference/model parallelism.

These are complementary capabilities. The existing team scheduler should remain useful even if Grid is added.

---

## 3. Is Aggregate Memory Technically Possible?

Yes, but not as simply as adding RAM/VRAM numbers together.

For example, two machines with 4 GB of usable model memory do not automatically behave like one perfect 8 GB device. Distributed execution also requires memory for:

- runtime overhead,
- KV cache,
- compute buffers,
- activations,
- networking buffers,
- framework/process overhead.

However, model-parallel systems can partition model weights and related runtime state across devices so a model may fit when no single device can hold it.

Current examples:

- `llama.cpp` has an RPC backend that can expose remote GGML devices and distribute model weights and KV cache across local and remote devices. The upstream project currently labels this RPC backend proof-of-concept, fragile, and insecure for untrusted networks.
- exo is specifically designed to split models across heterogeneous consumer devices. Its default strategy assigns model layers roughly according to device memory.
- vLLM supports multi-node single-model inference using tensor parallelism and pipeline parallelism, although its multi-node deployment model is primarily aimed at GPU server environments with consistent execution environments.
- DeepSpeed supports model-parallel inference for models that do not fit on one GPU.

Therefore the fundamental idea is proven. The research question for Toskar is whether it can make this reliable, secure, performant, and simple across consumer hardware.

---

## 4. Candidate Parallelism Strategies

Toskar should not assume that one distributed strategy will work for every hardware/network combination.

### 4.1 Pipeline / Layer Parallelism

Split the transformer into consecutive groups of layers.

```text
Prompt
  │
  ▼
Node A
Layers 0-9
  │ activations
  ▼
Node B
Layers 10-19
  │ activations
  ▼
Node C
Layers 20-29
  │
  ▼
Output
```

#### Advantages

- Natural way to aggregate memory across machines.
- Each node can hold only the layers assigned to it.
- Communication occurs between pipeline stages rather than requiring every device to synchronize every tensor operation.
- More plausible over ordinary LAN networking than fine-grained tensor parallelism.
- Can accommodate devices with different memory capacities by assigning different numbers of layers.

#### Disadvantages

- Each generated token must pass through the pipeline.
- The slowest stage can limit overall token generation.
- Network latency is directly visible in inference latency.
- Heterogeneous compute speeds make balancing difficult.
- Failure of one node breaks the model instance.

This is likely one of the most important approaches for Toskar to research first.

---

### 4.2 Tensor Parallelism

Split individual tensors/model operations across multiple devices.

Conceptually:

```text
Layer N
 ├── shard -> GPU A
 ├── shard -> GPU B
 └── shard -> GPU C

results synchronize
       │
       ▼
Layer N+1
```

#### Advantages

- Established technique for large-model inference.
- Can provide strong performance on tightly connected GPUs.
- Supported by systems such as vLLM and DeepSpeed.

#### Disadvantages

- Requires frequent synchronization.
- Network bandwidth and latency become critical.
- Designed most naturally for fast GPU interconnects such as NVLink, InfiniBand, or high-speed datacenter networking.
- Commodity Wi-Fi or 1 Gb Ethernet may make it slower than using a smaller model on one machine.
- Heterogeneous GPU architectures may complicate or prevent efficient tensor parallelism.

Tensor parallelism may be valuable for high-end homogeneous Toskar grids, but it should not be assumed to be the default consumer strategy.

---

### 4.3 llama.cpp RPC / Remote GGML Devices

`llama.cpp` currently includes an RPC backend capable of exposing accelerator devices from remote machines.

A coordinator can run `llama-server` or `llama-cli` while remote machines run `ggml-rpc-server`.

The upstream documentation states that model weights and KV cache are distributed across available local and remote devices in proportion to available memory, with manual tensor split options available.

This is highly relevant because Toskar already treats llama.cpp as an important local inference backend.

Potential Toskar architecture:

```text
Norn
  │
  ├── choose participating nodes
  ├── calculate memory split
  └── start distributed runtime
           │
           ▼
     llama.cpp coordinator
       │       │       │
       ▼       ▼       ▼
   RPC node  RPC node  local device
```

#### Major caution

The llama.cpp project currently describes the RPC backend as proof-of-concept, fragile, and insecure for open or sensitive networks.

Toskar should not expose raw llama.cpp RPC directly as a trusted cluster protocol without additional isolation/security design.

The RPC backend should initially be treated as an experiment and possible execution backend rather than the permanent Toskar Grid protocol.

---

### 4.4 Peer-to-Peer Layer Partitioning

Projects such as exo demonstrate a different model: peer devices form a distributed network and run portions of a model based on memory/capability.

This approach is especially interesting for Toskar because the product goal is heterogeneous consumer devices rather than datacenter-only clusters.

Potential lessons to research:

- weighted layer placement,
- automatic topology discovery,
- peer-to-peer scheduling,
- heterogeneous Apple/NVIDIA/CPU execution,
- automatic model partition planning,
- handling nodes joining/leaving,
- local API abstraction over a distributed model.

Toskar does not necessarily need to embed exo. It may instead use it as a reference architecture, integration backend, or source of design lessons.

---

### 4.5 Expert Parallelism / Mixture-of-Experts

Mixture-of-Experts models activate only portions of the model for a token.

In principle, experts can be distributed across devices.

This may eventually fit the Grid concept particularly well because model parameters can be placed on different machines without every machine evaluating every layer/expert.

However, this introduces additional routing and network requirements and should be considered later than basic layer partitioning.

---

## 5. Network Performance Is a First-Class Resource

For Grid mode, Toskar cannot schedule based only on:

- RAM,
- VRAM,
- CPU,
- GPU.

It must also understand the network.

Potential network characteristics:

```text
Node A <-> Node B
Latency:      0.7 ms
Bandwidth:    9.4 Gbps
Transport:    10 Gb Ethernet
Reliability:  High
```

Grid scheduling should eventually treat network links similarly to compute resources.

### Candidate network classes to test

- Wi-Fi 5
- Wi-Fi 6 / 6E
- Wi-Fi 7
- 1 Gb Ethernet
- 2.5 Gb Ethernet
- 5 Gb Ethernet
- 10 Gb Ethernet
- Thunderbolt networking between Macs
- high-speed USB networking where applicable
- InfiniBand / RoCE / RDMA on advanced Linux systems

The question is not merely:

> "Can it run?"

but:

> "Is running the larger distributed model actually preferable to running a smaller local model?"

---

## 6. Heterogeneous Hardware

This is where Toskar could differentiate itself from conventional distributed inference systems.

A home/office Toskar team might look like:

```text
M4 Max MacBook      48 GB unified memory
M5 Pro MacBook      24 GB unified memory
Linux workstation   RX 7900 XTX / 24 GB
Gaming PC           RTX 4080 / 16 GB
Mini PC             CPU / 32 GB
```

A useful Grid cannot assume all nodes have:

- identical GPUs,
- identical memory,
- identical operating systems,
- identical inference backends,
- identical performance.

Research questions:

1. Can one model realistically span different accelerator backends?
2. Can Metal, CUDA, ROCm, Vulkan, and CPU shards participate in one inference graph?
3. If not, should Grid initially require backend-compatible node groups?
4. How should Norn account for a very slow node?
5. When does adding a node make performance worse?
6. Can Grid automatically reject a node whose contribution would reduce useful performance?
7. Should memory-rich CPU nodes be allowed as overflow storage/compute?
8. How should context/KV cache be partitioned?

---

## 7. Proposed Toskar Abstraction

Do not make the user configure distributed inference terminology.

Introduce a logical resource:

## Grid

A Grid is a set of Toskar nodes that can cooperate to host one model.

Conceptually:

```text
Team
├── MacBook
├── Workstation
├── Mac Mini
└── Gaming PC

Grid planner
     │
     ▼
Compatible execution set
├── MacBook
├── Mac Mini
└── Workstation
     │
     ▼
Distributed Model Instance
```

Possible model fit labels:

```text
Excellent fit — This computer
Good fit      — This computer
Tight fit     — This computer
Grid fit      — 2 computers
Grid fit      — 3 computers
Doesn't fit   — Available hardware insufficient
```

Potential UI:

```text
Llama 70B Q4

Doesn't fit on Michael's MacBook
✓ Fits on your team

Recommended grid:
  MacBook Pro       18 GB
  Workstation       22 GB
  Mac Mini          10 GB

Network:
  2.5 Gb Ethernet
  Estimated: usable

Estimated generation:
  8-12 tok/s

[Run on team]
```

The user should not need to know whether the backend chose pipeline parallelism, RPC, or another partition mechanism.

---

## 8. Relationship to Existing Toskar Subsystems

### Norn

Norn becomes the Grid planner/scheduler.

Responsibilities may include:

- selecting candidate nodes,
- checking runtime/backend compatibility,
- measuring available memory,
- measuring link quality,
- choosing a partitioning strategy,
- calculating shard sizes,
- starting the distributed model,
- tracking shard ownership,
- routing requests to the logical model instance.

### Bifrost

Bifrost provides secure node connectivity and topology information.

Future Grid requirements may include:

- authenticated high-throughput node channels,
- encrypted control plane,
- optional optimized data-plane transport,
- topology measurement,
- bandwidth and latency tests,
- connection health.

Bifrost control traffic and model tensor/activation traffic may eventually need separate transport paths.

### Heimdall

Heimdall monitors:

- node health,
- shard health,
- coordinator health,
- network degradation,
- stalled inference,
- memory pressure,
- process failure.

A distributed model should expose one logical health state while retaining per-node diagnostics.

### Runtime layer

Grid support should remain backend-specific behind an abstraction.

Conceptually:

```go
type DistributedRuntime interface {
    ProbeGrid(nodes []Node) GridCapability
    Plan(model Model, nodes []Node) (GridPlan, error)
    Start(plan GridPlan) (DistributedModelInstance, error)
    Stop(instanceID string) error
    Health(instanceID string) GridHealth
}
```

This lets Toskar experiment with:

- llama.cpp RPC,
- exo,
- vLLM/Ray,
- future MLX distributed runtimes,
- custom Toskar execution.

---

## 9. Proposed Grid Plan

A persisted or inspectable Grid plan might contain:

```text
GridPlan
  model
  model_revision
  quantization
  strategy
  coordinator_node
  participating_nodes[]

NodeShard
  node_id
  runtime
  device
  memory_budget
  layer_range / tensor_share / role

NetworkRequirements
  measured_latency
  measured_bandwidth
  minimum_bandwidth
  topology

Context
  kv_cache_strategy
  context_length

HealthPolicy
  startup_timeout
  node_timeout
  recovery_policy
```

The normal UI should hide most of this.

Advanced mode can expose it for debugging and tuning.

---

## 10. Failure Semantics

Distributed inference creates failure modes that do not exist in single-node execution.

Examples:

- one node sleeps,
- laptop closes,
- Wi-Fi roams,
- Ethernet cable disconnects,
- remote GPU driver crashes,
- remote runtime is killed,
- memory pressure forces a shard out,
- network latency suddenly increases.

Research questions:

### Can a failed shard move?

If Node B disappears, can Toskar relocate its layers to Node C without restarting the entire model?

Likely answer for early implementations: no.

Initial policy may be:

```text
Grid node failed
     ↓
Stop distributed model instance
     ↓
Clean up all shards
     ↓
Re-plan using remaining nodes
     ↓
Reload model
     ↓
Offer retry
```

Later systems might support shard replication or migration.

### What happens to an active response?

Preserve partial output, mark the response interrupted, clean up the model, and explain which Grid member was lost.

This should integrate with the model health-monitoring work already planned for Toskar.

---

## 11. Scheduling Goals

The scheduler should optimize for more than "largest model possible."

Candidate objectives:

### Fit-first

Run a model that otherwise cannot run.

### Latency-first

Choose nodes and partitioning that minimize per-token delay.

### Throughput-first

Optimize multiple simultaneous users/requests.

### Power-first

Avoid waking high-power machines unless necessary.

### Stability-first

Leave substantial memory/network headroom.

A user-facing setting might eventually be as simple as:

```text
Grid optimization

○ Automatic
○ Fastest
○ Largest models
○ Lowest power
```

Norn translates that into the technical plan.

---

## 12. The Kubernetes Analogy — and Where It Breaks

The Kubernetes analogy is useful at the product level:

> Multiple machines become one managed pool of resources.

But distributed model inference is more tightly coupled than normal Kubernetes workloads.

Kubernetes commonly schedules relatively independent containers to nodes. A Grid model may require participating machines to exchange data every token and, with tensor parallelism, potentially many times per layer.

Therefore:

```text
Kubernetes:
"Which machine should run this workload?"

Toskar Team:
"Which machine should run this model/request?"

Toskar Grid:
"How should this single computation be partitioned across machines?"
```

Grid planning is therefore closer to HPC/model-parallel scheduling than ordinary container placement.

Toskar's opportunity is to make that complexity feel Kubernetes-like from the user's perspective even though the implementation is substantially more coupled.

---

## 13. Security Requirements

Distributed inference should not weaken Toskar's local-first security model.

Requirements to investigate:

- authenticated nodes,
- encrypted control traffic,
- encrypted model/inference traffic when practical,
- no unauthenticated raw RPC ports exposed on LAN,
- authorization for joining a Grid,
- protection from malicious nodes,
- no accidental Internet exposure,
- clear trust boundary for third-party runtime backends.

The current llama.cpp RPC documentation explicitly warns that its RPC backend is proof-of-concept and insecure for open/sensitive networks. Toskar should therefore wrap or isolate such mechanisms rather than exposing them directly as the user-facing trust model.

---

## 14. Model Licensing

Distributed loading can raise additional model-license questions.

Research should verify:

- whether a model's license allows use across multiple devices,
- whether remote-node distribution counts as redistribution in particular scenarios,
- whether model files must exist on every node,
- whether shards can be cached remotely,
- whether downloaded models can be transferred between Toskar nodes.

Do not assume all Hugging Face model licenses permit identical behavior.

This should become part of model metadata and Grid eligibility.

---

## 15. Product Questions

Before implementation, answer:

1. Is the main value **running models that otherwise do not fit**, or increasing speed?
2. Is Grid an Advanced feature initially?
3. Do users explicitly create Grids, or does Norn form them automatically?
4. Should Grid require wired networking for the first supported release?
5. Should mixed OS/hardware clusters be supported initially?
6. Which runtime becomes the first Grid backend?
7. Should model discovery show "Fits on team" before Grid is enabled?
8. Should Toskar automatically benchmark inter-node links?
9. When should Toskar recommend a smaller single-node model instead?
10. How much slower than local inference is still considered acceptable?

---

## 16. Recommended Research Phases

### Phase R0 — Baseline measurements

Create a repeatable benchmark harness.

Measure:

- model load time,
- prompt processing,
- time to first token,
- tokens/sec,
- peak memory per node,
- network throughput,
- network traffic per generated token,
- failure behavior.

Test on one machine first to establish baseline.

### Phase R1 — llama.cpp RPC proof of concept

Use two machines.

Suggested configurations:

- two Apple Silicon Macs,
- two NVIDIA/Linux machines,
- one local + one remote accelerator where supported.

Questions:

- Can a model too large for either individual machine load?
- How are weights and KV cache split?
- How does `--tensor-split` affect placement?
- What is the network traffic pattern?
- How much slower is token generation?
- What happens when the remote node disappears?
- Can Toskar safely supervise the RPC processes?

Do not expose this as a production feature.

### Phase R2 — Network matrix

Run the same model/configuration over:

- Wi-Fi,
- 1 GbE,
- 2.5 GbE,
- 10 GbE,
- Thunderbolt networking where available.

Determine minimum acceptable tiers.

### Phase R3 — exo evaluation

Evaluate exo as:

1. an execution backend,
2. a reference implementation,
3. a source of partition-planning ideas.

Measure heterogeneous device behavior.

### Phase R4 — vLLM/Ray evaluation

Test a homogeneous NVIDIA/Linux environment.

Determine whether it makes sense as an advanced/server-class Toskar Grid backend.

### Phase R5 — Norn Grid planner prototype

Implement a backend-independent Grid plan object.

Norn should determine:

- eligible nodes,
- aggregate usable memory,
- network quality,
- backend compatibility,
- expected bottleneck,
- selected execution strategy.

No UI commitment yet.

### Phase R6 — Product prototype

Expose a hidden/experimental UI:

```text
Experimental: Run across team
```

Only after reliability and performance thresholds are understood.

---

## 17. Initial Test Matrix

Suggested starting hardware:

```text
Test A
2 x Apple Silicon Macs
wired network

Test B
2 x NVIDIA Linux systems
wired network

Test C
Apple Silicon + NVIDIA
heterogeneous experiment

Test D
Apple Silicon + CPU-only node

Test E
same pair over Wi-Fi vs Ethernet
```

Models should include:

- a model that fits comfortably on one machine,
- a model that barely fits on one machine,
- a model that does not fit on either machine but fits in aggregate,
- progressively larger context lengths.

This allows Toskar to distinguish **distributed overhead** from **distributed necessity**.

---

## 18. Success Criteria for Moving from Research to Feature Design

Do not turn Grid into a committed feature until research demonstrates:

1. A model can reliably run across at least two Toskar nodes.
2. The model can be larger than what either individual node can run safely.
3. Startup and cleanup can be supervised automatically.
4. A node failure is detected and cleaned up without leaving orphan processes.
5. Network suitability can be measured before model launch.
6. Toskar can predict memory placement reasonably well.
7. At least one supported networking tier provides acceptable generation performance.
8. The distributed backend can be secured behind Toskar's trust model.
9. The UX can hide backend-specific configuration from normal users.
10. Distributed execution provides meaningful value over simply selecting a smaller local model.

---

## 19. Non-Goals During Research

Do not initially attempt:

- transparent live shard migration,
- Internet/WAN distributed inference,
- arbitrary untrusted community nodes,
- large-scale datacenter orchestration,
- distributed model training,
- every runtime/backend simultaneously,
- perfect heterogeneous tensor parallelism,
- Kubernetes integration itself.

The objective is to determine whether a small trusted LAN of Toskar computers can act as one useful inference resource.

---

## 20. Potential Future UX

If successful, the ultimate experience might be:

```text
Models
──────────────────────────────────

Llama 70B Q4
Large general-purpose model

This computer:   Too large
Your team:       Good grid fit

Uses:
✓ MacBook Pro
✓ AI Workstation
✓ Mac Mini

Estimated combined model memory: 51 GB
Network: Good
Estimated speed: ~10 tok/s

[Run on team]
```

And in Computers:

```text
Your AI team
3 computers · 88 GB memory

Grid capacity
51 GB currently available

Running
Llama 70B
Distributed across 3 computers
```

Normal users should never need to configure shards, ranks, RPC ports, tensor splits, or pipeline stages.

---

## 21. Strategic Value

Grid mode could become a major Toskar differentiator.

Many local-AI products answer:

> "What model can this computer run?"

Toskar could eventually answer:

> "What model can all of your computers run together?"

That aligns strongly with the existing product vision: Toskar should turn hardware the user already owns into a managed local AI platform.

The existing Team functionality is the natural foundation. Team routing can evolve into two complementary execution modes:

```text
Toskar Team

Mode A — Workload Grid
Different models/jobs run on different computers.

Mode B — Model Grid
One model spans multiple computers.
```

The user sees one team. Norn chooses the appropriate execution model.

---

## 22. Current External Technologies to Evaluate

### llama.cpp RPC

Relevant because Toskar already targets local GGUF/llama.cpp workflows.

Current upstream RPC documentation:
https://github.com/ggml-org/llama.cpp/blob/master/tools/rpc/README.md

Important current characteristics:

- remote GGML devices,
- multiple remote servers,
- automatic memory-proportional weight/KV distribution,
- configurable tensor split,
- local tensor cache support,
- optional RDMA support on suitable Linux hardware,
- currently explicitly described upstream as proof-of-concept and insecure for sensitive/open networks.

### exo

Project:
https://github.com/exo-explore/exo

Relevant characteristics:

- distributed inference across consumer devices,
- heterogeneous devices,
- peer-to-peer topology,
- memory-weighted model partitioning,
- automatic discovery,
- designed specifically to combine memory across devices.

### vLLM

Distributed inference documentation:
https://docs.vllm.ai/en/latest/serving/parallelism_scaling/

Relevant characteristics:

- tensor parallel inference,
- pipeline parallel inference,
- multi-node execution,
- Ray and native multiprocess distributed execution,
- primarily oriented toward GPU/server environments.

### DeepSpeed

Inference documentation:
https://www.deepspeed.ai/inference/

Relevant characteristics:

- model-parallel inference,
- automatic tensor parallelism,
- established multi-GPU/distributed model execution techniques.

---

## 23. Recommended Next Action

Do not build Grid yet.

Create a small engineering research spike around **llama.cpp RPC on two known Toskar nodes**.

The spike should answer one concrete question:

> Can Toskar automatically start a model that does not fit on either machine independently, split it across both machines, generate a useful response, measure the performance, and clean everything up afterward?

If yes, capture:

- memory distribution,
- network utilization,
- tokens/sec,
- first-token latency,
- startup time,
- cleanup behavior,
- failure behavior.

That experiment will provide much more useful information than designing the full Grid architecture prematurely.

---

## References

1. llama.cpp RPC backend documentation  
   https://github.com/ggml-org/llama.cpp/blob/master/tools/rpc/README.md

2. llama.cpp multi-GPU documentation  
   https://github.com/ggml-org/llama.cpp/blob/master/docs/multi-gpu.md

3. exo distributed inference project  
   https://github.com/exo-explore/exo

4. vLLM parallelism and scaling documentation  
   https://docs.vllm.ai/en/latest/serving/parallelism_scaling/

5. DeepSpeed inference documentation  
   https://www.deepspeed.ai/inference/

---

## Research Outcome

The desired end state is not merely "distributed inference."

The desired end state is:

> **Toskar presents all compatible computers as a managed AI grid, and Norn determines whether a request should run on one computer or whether one model should span several computers.**

The infrastructure may be complex. The product experience should not be.
