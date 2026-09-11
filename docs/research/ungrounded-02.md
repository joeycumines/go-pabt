# Proposed architecture: a rich, compiled BT model over the existing ABI

The strongest design is **not** to replace `go-behaviortree.Node`. It is to preserve the existing package as the minimal execution ABI and add an optional, immutable, serializable behavior-model layer that compiles into a high-performance runtime and can always be exposed as an ordinary legacy `Node`.

Conceptually:

```text
rich Definition
      │
      ├── reference interpreter
      ├── formal/planning tools
      └── optimizing compiler
               │
          shared Program
               │
       per-agent Instance
               │
          legacy bt.Node
```

The existing ABI remains:

```go
type Node func() (Tick, []Node)
type Tick func(children []Node) (Status, error)
```

with the existing `Running`, `Success`, and `Failure` statuses, constructors, traversal, decorators, manager, and ticker unchanged. ([GitHub][1])

There is one unavoidable qualification to “wholly compatible”:

> An arbitrary legacy closure may inspect time, globals, I/O, hidden mutable state, or dynamically construct children. No executor can safely stop invoking that closure unless it is told every condition that can change its result.

Therefore the design provides:

1. **Exact compatibility**, preserving legacy invocation behavior.
2. **Incremental optimization**, enabled only where dependencies and side effects are declared.
3. **Verified incremental execution**, which rejects nodes whose wake-up conditions are incomplete.

That distinction is essential; hiding it would produce an API that is fast in demonstrations but semantically incorrect in real programs.

---

## 1. Compatibility contract

| Compatibility dimension       | Guarantee                                                                                                                                                     |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Existing source code          | The root package is unchanged. Existing imports and calls compile without modification.                                                                       |
| Rich tree used by legacy code | A compiled `Instance` can be exposed as one ordinary `bt.Node`; existing managers, tickers, decorators, and parents can invoke it.                            |
| Legacy node used by rich code | Any old `bt.Node` can be embedded as an opaque action island. A factory is required when each agent needs an independent closure instance.                    |
| Observable execution          | `LegacyExact` preserves child order, invocation count relative to external ticks, status, error, and default panic propagation.                               |
| Structural tooling            | The rich logical tree is exposed through the existing `Metadata` and `WithStructure` mechanism even when the physical legacy representation is a single node. |
| Serialization                 | Rich definitions are serializable. Arbitrary Go closures are never serialized; they are referenced through registered, versioned implementation IDs.          |
| Optimized execution           | A node is skipped only when it declares purity or a complete event/timer/future dependency contract. Otherwise it remains polling-compatible.                 |
| Runtime state                 | Mutable execution state is per `Instance`; immutable definitions and compiled programs may be shared safely by many agents.                                   |

The current metadata API is particularly well suited to this bridge. `Metadata` provides values plus logical children, and `WithStructure` explicitly permits a conceptual tree to differ from its physical closure structure. ([GitHub][2])

The existing `Node.Value` mechanism should **not** become the rich runtime’s blackboard. Its documentation positions values primarily at API boundaries, and its implementation uses contextual synchronization machinery that is inappropriate for every hot-path read. Use it only for selected compatibility projections or a handle to the rich definition. ([GitHub][3])

---

## 2. Correcting the video-game framing

Your framing is directionally correct, but it combines two distinct optimizations:

### Event-driven scheduling is the principal work-elimination mechanism

A condition or guard declares what data it observes. When that data changes, the runtime schedules the affected observer, which may then abort its own subtree or a lower-priority active subtree. Unchanged portions of the tree do no work.

Unreal documents this directly: its behavior trees are event-driven rather than continuously traversed, use observers to abort lower-priority work, and separate shared node definitions from per-agent blackboard or node memory. Unity’s current Behavior package likewise describes event-driven execution and observer-abort behavior. These ideas also appear in earlier game-AI work using blackboards, dynamically attached behaviors, and “enticers.” ([Epic Games Developers][4])

### Linearization is a complementary data-layout optimization

The compiler should flatten the logical tree into compact arrays containing:

```text
opcode/type
parent
first child
child count
subtree end
configuration slot
state-bank index
source NodeID
flags and capabilities
```

That improves locality, makes subtrees contiguous intervals, accelerates priority aborts, and removes per-node pointer chasing. It does **not**, by itself, eliminate unnecessary execution. The reverse dependency index and ready queue do that.

The resulting design is hybrid:

```text
events / changed values / futures / timers
                 │
        reverse dependency index
                 │
             ready queue
                 │
      flat instruction program
                 │
       active-path state arrays
```

Some game conditions remain naturally polling-based—continuous physics queries, animation progress, or integrations without change notifications. Those nodes explicitly request `NextTick`, a periodic deadline, or a lower-frequency LOD timer.

Async actions should also remain cooperative rather than spawning a thread per BT node. BehaviorTree.CPP uses a single-threaded tree engine in which asynchronous actions quickly return `RUNNING`, later resume, and must support halting; its parallelism is logical concurrency rather than automatically assigning a thread to every child. ([BehaviorTree.CPP][5])

---

# 3. Package architecture

The root package must import none of the new packages:

```text
github.com/joeycumines/go-behaviortree
    Existing ABI; unchanged.

.../model
    Immutable definitions, typed node configurations, attachments,
    expressions, contracts, stable IDs, source locations.

.../exec
    Registry, compiler, reference interpreter, Program, Instance,
    event scheduler, blackboard, timers, futures, traces.

.../std
    Standard control, decorator, observer, temporal, utility,
    resource, and planning node descriptors and implementations.

.../compat
    Rich-to-legacy and legacy-to-rich adapters.

.../plan
    Symbolic, probabilistic, temporal, anytime, and multi-agent
    planning interfaces and candidate/certificate types.

.../verify
    Formal semantics, runtime monitors, model-checking exporters,
    counterexamples, assurance profiles.

.../codec
    Canonical language-neutral definition encoding, migrations,
    manifests, signatures, unknown-field preservation.

.../conformance
    Reference traces, wire-format vectors, semantic test suites.
```

A suitable dependency direction is:

```text
model
  ↑
exec ← std
  ↑
compat → legacy root

plan   → model
verify → model
codec  → model
```

`exec` may import the legacy root package for `Status`; the root package never imports `exec`.

Registries should be explicit:

```go
reg := exec.NewRegistry()
std.Register(reg)
game.Register(reg)
robot.Register(reg)
```

Avoid a mandatory process-global registry driven by `init`; it complicates tests, plugin conflict handling, deterministic builds, and security allowlists.

---

# 4. Separate the five kinds of “node data”

A complete API must not put every kind of data in one `map[string]any`.

### 4.1 Immutable authoring metadata

Examples:

* Human name and documentation.
* Tags and categories.
* Editor position and colour.
* Source file/span.
* Ownership and provenance.
* Debug breakpoints.
* UI presentation.

Presentation-only metadata should not invalidate compiled programs or formal proofs.

### 4.2 Immutable executable configuration

Examples:

* Retry count.
* Timeout duration.
* Utility expression.
* Guard-abort policy.
* Action implementation ID.
* Parallel success/failure thresholds.
* Subtree parameters.

This is part of the semantic hash.

### 4.3 Immutable symbolic and formal contracts

Examples:

* Preconditions and effects.
* Reads and writes.
* Action cost and duration.
* Resource claims.
* Outcome and observation models.
* Invariants.
* Cancellation postconditions.
* Determinism and side-effect classifications.

These are consumed by planners, conflict analysis, causal parallelization, runtime monitors, and verification backends.

### 4.4 Mutable per-instance execution state

Examples:

* Sequence cursor.
* Retry count.
* Active future ID.
* Utility hysteresis.
* Action-specific state.
* Activation generation.

This is never stored in the immutable definition and never shared accidentally between agents.

### 4.5 Dynamic world and blackboard state

Examples:

* Current target.
* Robot pose.
* Visibility facts.
* Inventory.
* Battery level.
* Shared fleet intentions.
* Events and sensor observations.

This belongs in typed, versioned stores or external bindings, with explicit scopes such as instance, entity, world, or fleet.

---

# 5. Core immutable model API

The canonical API should work on Go 1.26 and earlier supported versions without generic methods:

```go
package model

type Ref uint32
type NodeID [16]byte

type TypeID string
type KeyID string
type Version uint32

type Digest [32]byte

type Schema[T any] interface {
	EncodeCanonical(dst []byte, value T) ([]byte, error)
	Decode(data []byte) (T, error)
	Validate(value T) error
}

type Type[C any] struct {
	def *typeDef // immutable, unexported identity
}

type Key[T any] struct {
	def *keyDef // stable ID plus in-process identity
}

func DefineType[C any](
	id TypeID,
	version Version,
	schema Schema[C],
	options ...TypeOption[C],
) Type[C]

func DefineKey[T any](
	id KeyID,
	version Version,
	schema Schema[T],
	options ...KeyOption[T],
) Key[T]

type Builder struct {
	// Mutable construction state; not safe for concurrent use.
}

func Add[C any](
	b *Builder,
	typ Type[C],
	config C,
	children ...Ref,
) (Ref, error)

func Attach[T any](
	b *Builder,
	node Ref,
	key Key[T],
	value T,
) error

// These methods are legal before Go 1.27 because T/C belong to the receiver.
func (t Type[C]) AddTo(
	b *Builder,
	config C,
	children ...Ref,
) (Ref, error)

func (k Key[T]) AttachTo(
	b *Builder,
	node Ref,
	value T,
) error

type NodeView struct {
	// Immutable view into a Definition.
}

func Config[C any](node NodeView, typ Type[C]) (C, error)

func Attachment[T any](
	node NodeView,
	key Key[T],
) (T, bool, error)

func (k Key[T]) ReadFrom(node NodeView) (T, bool, error)

type Definition struct {
	// Representation unexported. No mutable slices are handed out.
}

func (b *Builder) Build(root Ref) (*Definition, Diagnostics)

func (d *Definition) Root() NodeView
func (d *Definition) SemanticHash() Digest
func (d *Definition) ArtifactHash() Digest
```

## Stable identity rules

Each node has a persistent `NodeID`, but runtime identity is:

```text
Definition hash + call path + NodeID
```

The call path matters because a reusable subtree definition may be invoked at several locations. Those invocations must not share sequence cursors or action state unless sharing is explicitly requested.

A separate definition digest prevents `NodeID` reuse from confusing caches or proof artifacts.

## Portable attachments versus local overlays

Portable node attachments require a stable key ID, schema version, canonical codec, and validation rule.

Process-local values—function pointers, editor handles, engine objects—should live in an immutable `Overlay` keyed by `NodeID`, not in the portable `Definition`:

```go
type Overlay struct {
	// Process-local, immutable after construction, not serialized.
}

func OverlaySet[T any](
	o *OverlayBuilder,
	node NodeID,
	key LocalKey[T],
	value T,
)
```

This preserves genuine cross-process and cross-language interoperability.

## Attachment policy

Each key should declare:

```go
type AttachmentDomain uint8

const (
	Semantic AttachmentDomain = iota
	Operational
	Presentation
)

type Inheritance uint8

const (
	NoInheritance Inheritance = iota
	InheritThroughSubtree
	InheritThroughCall
)

type MergePolicy uint8

const (
	Replace MergePolicy = iota
	Append
	SetUnion
	RejectConflict
)
```

Presentation attachments affect `ArtifactHash`; semantic attachments affect both `ArtifactHash` and `SemanticHash`. Proofs and compiled-program caches bind to the semantic hash.

Unknown attachments should round-trip through editors and codecs as raw versioned values. Unknown **behavior node types** may be inspected but cannot execute.

---

# 6. Go 1.27 generic methods: useful sugar, not the foundation

As of **July 12, 2026**, Go 1.27 is still an upcoming release with draft release notes and an expected August 2026 release; the current stable release is Go 1.26.5. The Go 1.27 draft supports type parameters on concrete methods, but interface methods still cannot declare type parameters, and generic methods cannot satisfy interface methods. The Go project also warns that tooling may need time to catch up. ([Go][6])

Therefore:

* Keep all canonical operations as generic free functions.
* Keep receiver-bound helpers such as `Key[T].ReadFrom`.
* Add Go 1.27 generic methods only as forwarding conveniences.
* Never require them in planner, registry, codec, or plugin interfaces.

A future build-tagged convenience file could contain:

```go
//go:build go1.27

package model

func (b *Builder) Add[C any](
	typ Type[C],
	config C,
	children ...Ref,
) (Ref, error) {
	return Add(b, typ, config, children...)
}

func (b *Builder) Attach[T any](
	node Ref,
	key Key[T],
	value T,
) error {
	return Attach(b, node, key, value)
}

func (node NodeView) Get[T any](
	key Key[T],
) (T, bool, error) {
	return Attachment(node, key)
}
```

The same rule applies to typed blackboard operations:

```go
// Stable API.
func Read[T any](ctx *Context, variable Var[T]) (T, bool)
func Write[T any](ctx *Context, variable Var[T], value T) error

// Go 1.27 convenience.
func (ctx *Context) Read[T any](variable Var[T]) (T, bool)
func (ctx *Context) Write[T any](variable Var[T], value T) error
```

The generic methods must contain no independent behavior; they simply forward to the stable functions.

---

# 7. Versioned node descriptors

Node semantics should be identified by open, namespaced IDs rather than a closed enum:

```text
behaviortree.std/sequence@1
behaviortree.std/selector@1
behaviortree.std/parallel@2
example.game/move-to@3
example.robot/grasp@2
```

Version should preferably remain a separate field rather than being parsed from the name.

A descriptor binds configuration schema, ports, arity, validation, static dependencies, and contracts:

```go
type Arity struct {
	Min uint32
	Max uint32 // MaxUint32 means unbounded
}

type Descriptor[C any] struct {
	Type  model.Type[C]
	Arity Arity
	Ports PortSchema

	Validate func(
		config *C,
		children []model.NodeView,
	) Diagnostics

	Contract func(config *C) model.Contract

	Capabilities CapabilitySet
}
```

Useful capabilities include:

```text
Pure
Deterministic
EventComplete
Cancelable
Snapshotable
SerializableState
Batchable
FormalModelAvailable
ThreadSafeExternalCalls
IdempotentAbort
```

Capabilities are claims that the conformance suite should be able to test. They are not merely documentation.

---

# 8. Standard node set

A rich standard library should distinguish semantics that many BT APIs ambiguously combine.

## Control flow

* Reactive Sequence.
* Memorizing Sequence.
* Reactive Fallback/Selector.
* Memorizing Fallback/Selector.
* Switch.
* Subtree Call.
* Dynamic Slot.
* Barrier.
* Race.

“Reactive” and “memorizing” should be separate descriptors or an explicit configuration field; users should never have to infer cursor behavior from a name.

## Parallel control

A `Parallel` configuration must specify all of:

```go
type ParallelConfig struct {
	Start       StartPolicy
	Success     Threshold
	Failure     Threshold
	Completion  CompletionPolicy
	Cancel      LoserCancellation
	Evaluation  EvaluationOrder
	Writes      ConcurrentWritePolicy
}
```

For example:

```text
Start: all children
Success: at least 2
Failure: at least 1
Completion: return as soon as either threshold is met
Cancel: abort unfinished children
Evaluation: stable child order
Writes: reject overlapping unmerged writes
```

Without these fields, two implementations that both call themselves “Parallel” are not interoperable.

## Reactivity and monitoring

* Guard.
* Observer.
* Monitor.
* Service.
* Invariant.
* Lower-priority abort.
* Self abort.
* Both/self-and-lower-priority abort.

A guard’s wake behavior must be explicit:

```go
type RecheckPolicy struct {
	Dependencies DependencySet
	Periodic     time.Duration
	EveryTick    bool
}
```

## Temporal and resilience decorators

* Timeout.
* Deadline.
* Retry with deterministic backoff.
* Repeat.
* Cooldown.
* Rate limit.
* Circuit breaker.
* Force success/failure.
* Invert.
* Cache with dependency invalidation.
* Debounce and stabilization windows.

Unbounded repeat/retry should be valid for ordinary execution but rejected by finite-verification profiles unless a bound is supplied.

## Selection

* Priority selector.
* Utility selector.
* Weighted random.
* Softmax/probabilistic selector.
* Multi-armed-bandit or learned selector as an optional plugin.

Utility selection needs hysteresis, minimum dwell time, and dependency declaration to prevent rapid thrashing.

Random nodes use injected deterministic streams derived from:

```text
root seed + runtime occurrence ID + activation generation
```

Every draw should be traceable for replay.

## Resources and coordination

* Acquire/Release.
* Scoped resource lease.
* Capacity semaphore.
* Mutex.
* Reservation.
* Multi-agent barrier.
* Intention publication.
* Backup/fallback policy.

A compiler should detect inconsistent lock ordering and overlapping parallel writes where possible.

## Deliberative nodes

* Goal.
* Plan.
* Replan/Repair.
* Information-gathering action.
* Desired-state/Active-Inference leaf.
* Runtime temporal monitor.

These are optional descriptors implemented through the same registry, not special cases in the legacy ABI.

---

# 9. Contracts shared by execution, planning, and verification

The same action contract should drive dependency scheduling, symbolic planning, conflict detection, causal parallelization, runtime monitoring, and formal abstraction:

```go
type Contract struct {
	Reads  ReadSet
	Writes WriteSet

	Preconditions Formula
	Outcomes      []Outcome
	Invariant     Formula

	Cost     CostModel
	Duration DurationModel

	Resources []ResourceUse

	Cancel CancelContract

	Volatility  Volatility
	Determinism DeterminismClass
	SideEffects SideEffectClass
}

type Outcome struct {
	When        Formula
	Probability ProbabilityModel
	Effects     []Effect
	Observes    []Observation
	Status      OutcomeStatus
}
```

`Formula`, `Effect`, and cost/duration models must be canonical data ASTs, not arbitrary Go predicates. Go functions may implement runtime execution, but the portable model needs an inspectable representation.

A suitable volatility classification is:

```go
type Volatility uint8

const (
	Pure Volatility = iota
	Observable // Complete variable/topic/future dependencies.
	Periodic   // Requires a timer or cadence.
	Opaque     // Must be invoked according to compatibility ticks.
)
```

A node may also dynamically narrow its wake set at runtime, but dynamic observations must remain within its declared static superset in strict mode. Debug builds should detect undeclared reads.

---

# 10. Compiled runtime API

```go
package exec

type CompileMode uint8

const (
	LegacyExact CompileMode = iota
	Incremental
	VerifiedIncremental
)

type CompileOptions struct {
	Mode         CompileMode
	Determinism  DeterminismProfile
	Verification VerificationProfile
	Tracing      TraceOptions
}

type Program struct {
	// Immutable flat instructions, config banks, source maps,
	// dependency indexes, and state-bank descriptions.
}

func Compile(
	definition *model.Definition,
	registry *Registry,
	options CompileOptions,
) (*Program, Diagnostics)

type Instance struct {
	// Mutable state for one logical agent/runtime occurrence.
}

type Bindings struct {
	// Typed action services, world stores, clocks, RNG roots,
	// resource managers, external topic adapters.
}

func NewInstance(
	program *Program,
	bindings Bindings,
	options ...InstanceOption,
) (*Instance, error)

type Budget struct {
	Transitions uint64
	Callbacks   uint64
	Deadline    time.Time // Optional nondeterministic wall-clock budget.
}

type YieldReason uint8

const (
	Completed YieldReason = iota
	AwaitingEvent
	AwaitingTimer
	AwaitingFuture
	BudgetExhausted
)

type Result struct {
	Status    behaviortree.Status
	Err       error
	Reason    YieldReason
	Quiescent bool
	Steps     uint64
}

func (i *Instance) Advance(
	ctx context.Context,
	cause Cause,
	budget Budget,
) Result

func (i *Instance) Notify(event Event) error
func (i *Instance) Abort(cause AbortCause) Result
func (i *Instance) Snapshot() (Snapshot, error)
func (i *Instance) Restore(Snapshot) error
func (i *Instance) Inspect() InstanceView
```

For deterministic simulation, transition-count budgets are preferable to wall-clock budgets. Wall time can vary by machine, scheduler, and instrumentation.

## Internal status versus public status

The public status set remains exactly:

```text
Running
Success
Failure
```

Internally, an occurrence may be:

```text
Idle
Entering
Active
Suspended
Aborting
Completed
```

Cancellation is a lifecycle transition, not a fourth public BT status. An error maps to `Failure` plus the existing error return unless an explicit error-policy decorator handles it.

---

# 11. Open handler SPI

Standard control nodes should compile to built-in opcodes. Custom actions and custom control operators use a lower-level state-machine SPI:

```go
type SignalKind uint8

const (
	EnterSignal SignalKind = iota
	ExternalTickSignal
	ChildCompletedSignal
	DependencyChangedSignal
	TopicSignal
	TimerSignal
	FutureSignal
	AbortSignal
)

type Signal struct {
	Kind SignalKind

	Child  uint32
	Status behaviortree.Status
	Err    error

	Cause Cause
}

type Completion struct {
	Status behaviortree.Status // Success or Failure.
	Err    error
}

type Commands struct {
	Invoke   ChildSet
	Cancel   ChildSet
	Complete *Completion
	Suspend  WakeSet
}

type Handler[C, S any] struct {
	NewState func(config *C) (S, error)

	OnSignal func(
		ctx *Context,
		config *C,
		state *S,
		signal Signal,
	) Commands

	Snapshot func(state *S) ([]byte, error)
	Restore  func(config *C, data []byte) (S, error)
	Migrate  StateMigration
}

func Register[C, S any](
	registry *Registry,
	descriptor model.Descriptor[C],
	handler Handler[C, S],
) error
```

The generic registration function creates a type-erased internal machine while retaining:

* A typed immutable configuration bank.
* A typed `[]S` state bank per instance.
* Correct Go garbage-collector visibility.
* No `unsafe` casting of arbitrary state into byte arenas.

Built-in opcodes can bypass virtual dispatch; plugins retain full extensibility.

---

# 12. Wake sets and typed dynamic state

A running action should say exactly what can wake it:

```go
type WakeSet struct {
	Vars     VarSet
	Topics   TopicSet
	Futures  FutureSet
	Deadline time.Time

	NextExternalTick bool
}
```

Examples:

```text
MoveTo:
    Running; wake on NavResult future or cancellation.

WaitUntilVisible:
    Running; wake when Visible or Target changes.

Animation:
    Running; wake on animation completion topic.

Continuous steering:
    Running; wake on next simulation tick.

Cooldown:
    Running; wake at a specific monotonic deadline.
```

Typed variables and topics avoid stringly typed blackboards:

```go
type Var[T any] struct {
	def *varDef
}

type Topic[T any] struct {
	def *topicDef
}

type Future[T any] struct {
	id FutureID
}

func DefineVar[T any](
	id VarID,
	schema model.Schema[T],
	options ...VarOption[T],
) Var[T]

func Read[T any](
	ctx *Context,
	variable Var[T],
) (T, bool)

func Observe[T any](
	ctx *Context,
	variable Var[T],
) (T, bool)

func Write[T any](
	ctx *Context,
	variable Var[T],
	value T,
) error

func EventOf[T any](
	topic Topic[T],
	value T,
) (Event, error)
```

`Read` performs a snapshot read. `Observe` additionally records a dynamic wake dependency. In `VerifiedIncremental`, observed variables must be included in the handler’s declared dependency superset.

## Transactional writes

Writes are staged and committed at a defined microstep boundary:

1. Handler sees a stable snapshot.
2. Handler emits writes and commands.
3. Runtime validates and commits writes.
4. Changed-variable events are coalesced.
5. Observers are scheduled for a later microstep, avoiding reentrant execution.

Parallel branches writing the same variable must either:

* Be rejected.
* Have a stable ordered-commit policy.
* Use a registered deterministic merge operator.
* Be proven mutually exclusive.

---

# 13. Flat program and per-agent instance layout

A compiled instruction can be approximately:

```go
type instruction struct {
	opcode uint32

	parent      int32
	firstChild  uint32
	childCount  uint16
	flags       uint16
	subtreeEnd  uint32

	configClass uint16
	configIndex uint32

	stateClass uint16
	stateIndex uint32

	sourceIndex uint32
}
```

The immutable `Program` contains:

* Depth-first instruction array.
* Configuration banks.
* Node and source maps.
* Child ranges.
* Subtree intervals.
* Reverse variable/topic dependency indexes.
* Guard priority and abort intervals.
* Static read/write conflict information.
* State-bank allocation descriptions.
* Optional batch-kernel descriptors.
* Semantic and compiler hashes.

The mutable `Instance` contains:

* Per-node status and phase arrays.
* Sequence/selector cursors.
* Activation generations.
* Active and dirty bitsets.
* Ready deque.
* Timer-wheel entries.
* Future subscriptions.
* Typed handler-state banks.
* Activation stack.
* Blackboard version.
* Trace/replay cursor.

This follows the same high-level separation used by mature game BT systems: shared node definitions plus per-agent state. ([Epic Games Developers][7])

## Expected work profile

The intended performance targets are:

```text
No pending event:
    O(1) quiescence check.

One changed variable:
    O(number of watchers + affected active paths).

Subtree abort:
    O(number of active occurrences in the subtree).

Legacy opaque tree:
    Original polling complexity.

Many agents:
    Optional batching by (Program, opcode/handler).
```

These are architectural targets rather than unconditional asymptotic promises; dynamic handlers and external systems can dominate costs.

---

# 14. Multi-agent and ECS scheduling

A second-level scheduler can group ready operations across instances:

```go
type World struct {
	// Programs, instances, event queues, batch registries.
}

func (w *World) Advance(
	ctx context.Context,
	events []Event,
	budget WorldBudget,
) WorldResult
```

Batch keys may include:

```text
Program ID
opcode/handler ID
configuration class
ECS archetype
execution priority
```

A handler may optionally provide:

```go
type BatchHandler[C, S any] struct {
	Evaluate func(
		ctx *BatchContext,
		configs []C,
		states []S,
		instances []InstanceID,
	) []Commands
}
```

This supports:

* SIMD-friendly conditions.
* ECS query batching.
* Batched navigation requests.
* Crowd perception.
* Job-system integration.
* Per-LOD execution budgets.

Every `Instance` remains logically single-writer. External threads may enqueue events through an MPSC queue, but only the owning executor mutates node state. That provides deterministic transition ordering without putting a mutex around every node.

Distributed robot fleets need a different profile: shared resources use leases or reservations, actions are idempotent where possible, and events carry causal IDs. A distributed execution cannot claim the same global determinism as a centralized game simulation without stronger synchronization assumptions.

---

# 15. Determinism, async execution, and cancellation

A production runtime should make these rules normative:

1. Events receive monotonically ordered sequence numbers at an instance boundary.
2. Equal-priority events use stable tie-breaking.
3. Clocks and random sources are injected.
4. Random draws are recorded.
5. Writes commit at explicit microstep boundaries.
6. Async completion includes the node’s activation generation.
7. Completion from an older generation is discarded as stale.
8. Abort is idempotent.
9. Descendants abort in reverse activation order.
10. Resource leases release on completion, abort, or instance destruction.
11. An async operation may not retain a `*Context`; it posts a future or topic event.
12. The default legacy adapter does not recover panics, preserving legacy behavior.

An async action typically follows:

```text
Enter
  ├── start external operation
  ├── record future ID
  └── Suspend(future)

Future completion
  ├── verify activation generation
  ├── consume result
  └── Complete(Success or Failure)

Abort
  ├── cancel operation
  ├── invalidate generation
  └── release resources
```

Legacy actions without a cancellation hook are marked `NonCancelable`. The rich runtime can stop listening to them, but it cannot honestly claim that their external side effect has stopped.

---

# 16. Rich-to-legacy adapter

The principal adapter should be:

```go
package compat

func AsNode(
	instance *exec.Instance,
	options ...NodeOption,
) behaviortree.Node
```

Physically, it is one legacy node whose tick invokes `Instance.Advance`. Logically, it exposes the rich tree through a `bt.Metadata` view and the existing `WithStructure` facility.

That gives:

```text
legacy parent/decorator/manager
            │
       one physical Node
            │
       compiled Instance
            │
  full logical Metadata tree
```

Consequences:

* Existing execution code sees a normal node.
* Existing walkers and tooling can inspect the conceptual rich structure.
* The compiled runtime keeps its flat representation.
* The adapter does not recursively allocate one closure per rich node.

A second, less efficient adapter should be available:

```go
func Materialize(
	definition *model.Definition,
	registry *exec.Registry,
	options ...MaterializeOption,
) (behaviortree.Node, error)
```

`Materialize` produces a physical hierarchy of legacy nodes. It is useful for consumers that directly depend on physical child closures rather than logical metadata, but it generally sacrifices incremental scheduling and compact storage.

---

# 17. Legacy-to-rich adapter

```go
type LegacyOptions struct {
	Name string

	Factory func() behaviortree.Node

	Purity       Purity
	Dependencies exec.DependencySet
	Contract     *model.Contract

	Abort func(
		context.Context,
		exec.AbortCause,
	) error

	StaticStructure bool
	Shareable       bool
}

func LegacySpec(
	node behaviortree.Node,
	options ...LegacyOption,
) model.NodeSpec

func LegacyFactorySpec(
	factory func() behaviortree.Node,
	options ...LegacyOption,
) model.NodeSpec
```

Rules:

* A supplied `Node` is suitable for one instance unless explicitly declared shareable.
* A `Factory` is required for per-agent closure state.
* By default the node is `Opaque`, `NonCancelable`, and nonserializable.
* In exact mode it is invoked normally.
* In incremental mode it continues receiving external ticks unless complete dependencies are supplied.
* Existing metadata may be used for editor display, but it is not automatically interpreted as a formal action contract.
* Dynamic physical children are not flattened unless `StaticStructure` is asserted.
* A verifier treats an opaque node as nondeterministic over its declared abstract outcomes, or as an explicit unresolved assumption.

This is the only safe interoperability rule for arbitrary closures.

---

# 18. Planning API

The runtime should not hard-code PA-BT, OBTEA, UHBTP, or any other named algorithm. It should expose a planning substrate on which those algorithms are implementations.

```go
package plan

type ActionID string
type FluentID string
type ResourceID string

type ActionSchema struct {
	ID         ActionID
	Parameters []Parameter

	Contract model.Contract

	Executor model.TypeID
}

type Domain struct {
	Types       []ObjectType
	Fluents     []Fluent
	Actions     []ActionSchema
	Resources   []Resource
	Axioms      []Formula
	Constraints []Formula
}

type Truth uint8

const (
	False Truth = iota
	True
	Unknown
)

type Knowledge interface {
	Truth(atom Atom) Truth
	Digest() model.Digest
}

type Problem struct {
	Domain  *Domain
	Initial Knowledge

	Goal    Formula
	Mission *LTLfFormula

	Constraints []Formula
	Objective   Objective
}

type Claims struct {
	Soundness    Claim
	Completeness Claim
	Optimality   Bound
	Grounding    GroundingClaim
}

type Candidate struct {
	Tree       *model.Definition
	Score      Score
	LowerBound Score

	Claims   Claims
	Evidence []EvidenceRef
	Stats    SearchStats
}

type Yield func(Candidate) bool

type Planner interface {
	Search(
		context.Context,
		Problem,
		Yield,
	) error
}
```

Streaming candidates supports:

* Optimal planners.
* Satisficing planners.
* Anytime improvement.
* Planner portfolios.
* Incremental repair.
* A running incumbent while a better candidate is being sought.

A candidate is committed only at a runtime safe point and only after structural, contract, security, and optional verification checks.

## Heuristics and LLM advisors

```go
type Estimate struct {
	Value      Score
	IsLowerBound bool
	Evidence   EvidenceRef
}

type Heuristic interface {
	Estimate(SearchNode) Estimate
}

type Hint struct {
	Action     ActionID
	Rank       float64
	Confidence float64
	Provenance Provenance
}

type Advisor interface {
	RankActions(
		context.Context,
		AdvisoryQuery,
	) ([]Hint, error)
}
```

An LLM should be an `Advisor`, goal parser, model proposer, or tie-breaker—not an execution authority.

It may propose:

* Symbol mappings.
* Candidate goal formulas.
* Action orderings.
* Decompositions.
* Missing action-model hypotheses.
* Heuristic paths.

The system must still validate:

* Symbol existence.
* Parameter types.
* Preconditions/effects.
* Resource constraints.
* Allowed action implementations.
* Formula complexity.
* Candidate tree structure.
* Planner claims.

An unproven LLM estimate cannot be treated as an admissible lower bound. An optimal search may use it for tie-breaking or ordering while retaining a separately justified bound.

---

# 19. Direct mapping of the research spectrum

| Research line                | API/architecture realization                                                                                                                                                                                                                                                   |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **PA-BT**                    | Incremental backward expansion and repair planner that adds subtrees for unsatisfied preconditions while the incumbent tree continues acting. PA-BT’s core contribution is reactive iterative expansion rather than separating planning and execution completely. ([arXiv][8]) |
| **BT Expansion**             | Complete state-space planner implementation with an explicit region-of-attraction result and machine-readable soundness/completeness claims. ([AAAI Publications][9])                                                                                                          |
| **OBTEA**                    | Uniform-cost backward planner over nonnegative action costs; emits exact cost and optimality evidence when its assumptions hold. ([IJCAI][10])                                                                                                                                 |
| **BTPG / UHBTP**             | Domain-independent heuristic plugin, including delete-relaxation or planning-graph-style estimates; candidate streaming supports its scalable and anytime role. The BTPG framework also motivates common planning/execution and robustness metrics. ([IJCAI][11])              |
| **HBTP**                     | `Advisor` or heuristic-path provider with separately selectable optimal and satisficing search policies; reflection and pruning remain planner behavior, not executor semantics. ([arXiv][12])                                                                                 |
| **Belief Behavior Trees**    | Three-valued facts, belief backends, explicit observation models, nondeterministic outcomes, and information-gathering actions. ([arXiv][13])                                                                                                                                  |
| **Active Inference + BTs**   | Desired-state leaf or local controller that chooses online actions under uncertainty. It is best used beneath or beside deliberative planning, not as a replacement for the execution kernel. ([arXiv][14])                                                                    |
| **CABTO**                    | Separate evidence for symbolic action-model coverage and for consistency between the declared effects and the grounded low-level policy. Proposal, policy sampling, and refinement fit a `Grounder` pipeline. ([AAAI Publications][15])                                        |
| **BehaVerify**               | Exporter from the normative BT semantics and leaf contracts to a finite-state model-checking representation, with source-mapped counterexamples. ([arXiv][16])                                                                                                                 |
| **Fiacre-based BTs**         | Timed/stateful formal exporter plus a runtime backend or monitor adapter, supporting offline temporal checking and deployment-oriented execution. ([arXiv][17])                                                                                                                |
| **LTLf synthesis**           | Mission formula in `Problem.Mission`; synthesis creates a BT whose successful traces satisfy the finite-trace temporal requirement, while leaf planners remain replaceable. ([arXiv][18])                                                                                      |
| **PDDL execution graphs**    | Causal-graph optimizer that transforms a valid sequential candidate into safe partial-order or parallel BT structure using read/write/resource dependencies. ([arXiv][19])                                                                                                     |
| **MRBTP**                    | Multi-agent planner with cross-tree expansion, intention sharing, backup structures, resource ownership, and optional long-horizon advisory subtrees. ([arXiv][20])                                                                                                            |
| **AS2FM-style verification** | Interoperation among statecharts, ROS 2, and BT models through a common formal representation and statistical model checking. ([arXiv][21])                                                                                                                                    |

Emerging multi-robot active-inference BT work can fit the same belief, distributed-intention, and probabilistic-outcome interfaces, but the cited IIBT work is presently identified as submitted rather than established peer-reviewed consensus. It should therefore remain an experimental plugin, not define the stable core semantics. ([arXiv][22])

The important architectural outcome is that **algorithms advertise guarantees rather than acquiring guarantees from their names**. Every result carries the assumptions under which soundness, completeness, optimality, or robustness is claimed.

---

# 20. Belief and probabilistic execution

A belief interface should support several backends:

```text
Exact symbolic state
Three-valued fact state
Factored categorical distributions
Gaussian or interval state
Particle belief
Externally managed estimator
```

The portable action model describes:

```go
type ObservationModel struct {
	Condition   Formula
	Distribution DistributionModel
	Updates     []BeliefUpdate
}

type Outcome struct {
	Probability ProbabilityModel
	Effects     []Effect
	Observation *ObservationModel
}
```

Execution does not require every game action to become probabilistic. A simple deterministic action uses a single outcome with probability one.

For robotics, a condition such as `ObjectLocated(mug)` can be `Unknown`. The planner may insert a sensing action rather than treating unknown as false. An active-inference leaf can perform local online control while the higher-level BT continues to enforce resources, mission sequencing, timeouts, and safety monitors.

Probability calibration and symbolic validity are separate:

```text
Symbolic model:
    Is this outcome allowed?

Probability model:
    How likely is it?

Grounding evidence:
    Does the deployed controller actually realize the outcome?

Runtime monitor:
    Did this execution remain within the allowed transition set?
```

---

# 21. Grounding and effect consistency

CABTO motivates a distinction that should be first-class in the API:

```go
type PolicyBinding struct {
	Action   plan.ActionID
	Executor model.TypeID

	Declared model.Contract
	Evidence GroundingEvidence
}

type GroundingEvidence struct {
	TaskCoverage      CoverageClaim
	EffectConsistency ConfidenceInterval

	EnvironmentDigest model.Digest
	PolicyDigest      model.Digest
	DataDigest        model.Digest

	Counterexamples []TraceRef
}
```

A `Grounder` may:

```go
type Grounder interface {
	Propose(
		context.Context,
		GroundingProblem,
		func(Proposal) bool,
	) error

	SamplePolicy(
		context.Context,
		Proposal,
		SamplingBudget,
	) (SamplingReport, error)

	Refine(
		context.Context,
		Proposal,
		SamplingReport,
	) (Proposal, error)
}
```

At runtime, an effect monitor compares observed transitions with the declared outcome set. A mismatch may:

* Fail the action.
* Trigger a safety fallback.
* Reduce confidence in the binding.
* Produce a counterexample.
* Start local PA-BT-style repair.
* Reinvoke a planner with updated knowledge.

The planner’s symbolic completeness must not be confused with the physical controller’s consistency.

---

# 22. Normative formal semantics

True interoperability requires a published transition system, not just Go interfaces.

A runtime state can be specified as:

```text
S = (
    program,
    node phases,
    child cursors,
    activation generations,
    ready queue,
    blackboard snapshot,
    pending transaction,
    timers,
    futures,
    resource leases,
    RNG state,
    event sequence,
    trace
)
```

A semantic microstep is:

```text
step(S, event) -> (S', emitted trace records)
```

The specification must fix:

* Child evaluation order.
* Reactive versus memorizing control behavior.
* Status propagation.
* Error propagation.
* Event ordering.
* Commit boundaries.
* Observer-abort priority.
* Parallel thresholds.
* Parallel cancellation.
* Retry and timeout behavior.
* Resource acquisition.
* Random-stream derivation.
* Timer interpretation.
* Fairness assumptions.
* Hot-reload boundaries.

The reference interpreter implements this specification directly and simply. The optimized flat executor is differential-tested against it.

## Formal backend SPI

```go
type VerificationRequest struct {
	Definition *model.Definition
	Profile    VerificationProfile
	Properties []Property
}

type Backend interface {
	Check(
		context.Context,
		VerificationRequest,
	) (VerificationReport, error)
}

type VerificationReport struct {
	Result       VerificationResult
	Assumptions  []Assumption
	Guarantees   []Guarantee
	Counterexamples []Counterexample
	Artifacts    []ArtifactRef
}
```

Recommended adapters include:

```text
verify/nuxmv
verify/fiacre
verify/jani
verify/ltlf
verify/runtime
```

Every opaque leaf must have one of:

1. A finite abstract transition model.
2. A bounded probabilistic model.
3. An explicit assumption.
4. An “unverified” designation that prevents a stronger certificate.

A formalization of stateful BTs shows why bounded profiles matter: sufficiently unrestricted blackboards, including unbounded integer state, can make the model computationally unbounded; finite-state model checking therefore needs bounded value domains, queues, timers, recursion, retries, and agent counts. ([arXiv][23])

A useful finite-verification profile might require:

```go
type VerificationProfile struct {
	MaxNodes       uint32
	MaxCallDepth   uint32
	MaxAgents      uint32
	MaxQueueLength uint32
	MaxRetries     uint32

	BoundedVariables bool
	BoundedTimers    bool
	NoDynamicTypes   bool
	NoOpaqueLeaves   bool
}
```

---

# 23. Certificates and proof invalidation

```go
type Certificate struct {
	SemanticsVersion string

	DefinitionHash model.Digest
	DomainHash     model.Digest
	CompilerHash   model.Digest

	PlannerID      string
	PlannerVersion string

	Assumptions []Assumption
	Guarantees  []Guarantee

	Optimality plan.Bound
	Grounding  []GroundingEvidence

	Properties []PropertyResult
	Counterexamples []Counterexample
}
```

A certificate is valid only for its exact semantic inputs. Changing any of these invalidates or narrows it:

* Semantic node configuration.
* Contracts.
* Action model.
* Runtime semantic version.
* Verification profile.
* Grounded policy.
* Relevant environment assumptions.

Moving a node in the visual editor should not invalidate it if editor coordinates are presentation-only.

---

# 24. Hot reload and dynamic behavior

Definitions and programs remain immutable. Hot reload creates a new definition and a transactional patch:

```go
type Patch struct {
	BaseHash model.Digest
	NextHash model.Digest
	Ops      []PatchOp
}

func Diff(
	oldDefinition *model.Definition,
	newDefinition *model.Definition,
) Patch

func (i *Instance) ApplyPatch(
	ctx context.Context,
	patch Patch,
	policy MigrationPolicy,
) PatchReport
```

Patch application occurs at a safe microstep boundary.

State is retained only when these are compatible:

```text
stable NodeID
same node TypeID
compatible config version
compatible state schema
same runtime call identity
```

Otherwise the runtime:

1. Aborts the old occurrence.
2. Invalidates its activation generation.
3. Releases resources.
4. Creates fresh state for the new occurrence.

A handler may provide a deterministic state migration. Removed nodes are always aborted before their state is discarded.

For games, dynamic composition should primarily use:

* Versioned subtree calls.
* Typed parameters.
* `Slot` nodes constrained to an allowed interface.
* Immutable overlays.
* Transactional patches.

This captures the flexibility of dynamic “enticer” systems without allowing arbitrary concurrent mutation of the active instruction array.

---

# 25. Serialization and interoperability

Serialize the `Definition`, never the compiled `Program`.

The wire model should include:

```text
semantic version
required capabilities
node IDs
type IDs and schema versions
canonical configurations
ordered children
portable attachments
subtree/module definitions
port and parameter bindings
contracts
source maps
semantic and artifact digests
```

The compiled `Program` is:

* Runtime-version-specific.
* Platform-optimized.
* Cacheable.
* Reconstructible from the definition and registry.
* Not an interchange format.

Interoperability requirements should include:

* A normative logical schema independent of encoding.
* Human-readable diagnostic encoding.
* Deterministic binary encoding.
* Unknown-field and unknown-attachment preservation.
* Stable ordering and canonical hashing.
* Migration test vectors.
* Standard-node semantic trace vectors.
* Capability negotiation.
* Explicit rejection of unsupported executable node types.
* No embedded arbitrary Go code.
* Optional signatures for deployed definitions and certificates.

---

# 26. Security boundary

Treat externally authored, downloaded, or LLM-generated definitions as untrusted data.

A deployment policy should constrain:

```go
type Policy struct {
	AllowedNodeTypes   TypeSet
	AllowedActions     ActionSet
	AllowedResources   ResourceSet
	AllowedSubtrees    SubtreeSet

	MaxNodes           uint32
	MaxDepth           uint32
	MaxParallelism     uint32
	MaxRetries         uint32
	MaxTimerHorizon    time.Duration
	MaxFormulaNodes    uint32

	RequireCancelable  bool
	RequireContracts   bool
	RequireCertificate bool
}
```

A tree may reference an allowlisted action ID; it must never be able to request arbitrary reflection-based function invocation or load a Go plugin by naming a filesystem path.

Planner and LLM provenance should be recorded, but provenance never substitutes for validation.

---

# 27. Video-game example

Suppose an NPC has:

```go
var Health = exec.DefineVar[float32](
	"game/npc/health",
	Float32Schema,
)

var PlayerVisible = exec.DefineVar[bool](
	"game/npc/player-visible",
	BoolSchema,
)

var Target = exec.DefineVar[EntityID](
	"game/npc/target",
	EntityIDSchema,
)

var NavigationDone = exec.DefineTopic[NavResult](
	"game/navigation/done",
	NavResultSchema,
)
```

The logical tree is:

```text
Priority Fallback
├── Guard: Health < 0.20
│   ├── observes Health
│   ├── abort self + lower priority
│   └── Flee
├── Guard: PlayerVisible && Target exists
│   ├── observes PlayerVisible, Target
│   ├── abort self + lower priority
│   └── Combat
└── Patrol
```

Construction could look like:

```go
var b model.Builder

flee, _ := std.Action.AddTo(
	&b,
	std.ActionConfig{
		Implementation: "game/flee@2",
	},
)

lowHealth, _ := std.Guard.AddTo(
	&b,
	std.GuardConfig{
		Predicate: expr.Less(
			expr.Variable(Health),
			expr.Constant(float32(0.20)),
		),
		Abort: std.AbortSelfAndLowerPriority,
	},
	flee,
)

combat, _ := std.Action.AddTo(
	&b,
	std.ActionConfig{
		Implementation: "game/combat@4",
	},
)

canFight, _ := std.Guard.AddTo(
	&b,
	std.GuardConfig{
		Predicate: expr.And(
			expr.Variable(PlayerVisible),
			expr.IsSet(expr.Variable(Target)),
		),
		Abort: std.AbortSelfAndLowerPriority,
	},
	combat,
)

patrol, _ := std.Action.AddTo(
	&b,
	std.ActionConfig{
		Implementation: "game/patrol@3",
	},
)

root, _ := std.Fallback.AddTo(
	&b,
	std.FallbackConfig{
		Memory: std.Reactive,
	},
	lowHealth,
	canFight,
	patrol,
)

definition, diagnostics := b.Build(root)
```

`Flee` or `Patrol` can start a navigation request and suspend on a future or `NavigationDone` topic. It is not called every frame merely to ask whether navigation has finished.

When health changes:

```text
Health write commits
    ↓
reverse index finds low-health guard
    ↓
guard reevaluates
    ↓
active combat/patrol interval is aborted
    ↓
flee action is entered
```

No unrelated condition is evaluated.

For thousands of NPCs:

* Compile one `Program`.
* Create one lightweight `Instance` per NPC.
* Store entity-specific data in bindings or ECS columns.
* Batch identical conditions and actions.
* Use immediate events for critical stimuli.
* Use timer cadence for low-importance ambient behavior.
* Coalesce repeated changes within a frame.
* Replicate semantic events, snapshots, and deterministic seeds for networked simulation rather than replicating a complete tree traversal.

---

# 28. Robotics example

Mission:

```text
Deliver the mug to the user,
never enter the restricted region,
keep the carried object stable,
and stop safely if localisation confidence is lost.
```

The problem model includes:

```text
Goal:
    Delivered(mug, user)

LTLf mission:
    always not InRestrictedZone(robot)
    and always (Carrying(mug) -> StableGrip(mug))
    and eventually Delivered(mug, user)

Initial belief:
    Location(mug) = Unknown
    Door(open) = False
    Battery = 0.72
```

Action schemas might be:

```text
DetectObject
Approach
OpenDoor
Grasp
Navigate
Place
Recharge
RequestHumanAssistance
```

Each action specifies:

* Symbolic preconditions/effects.
* Possible failure outcomes.
* Observations.
* Cost.
* Duration distribution or interval.
* Required arm/base/camera resources.
* Runtime implementation ID.
* Cancellation contract.
* Grounding evidence.

Execution pipeline:

```text
typed mission
    ↓
symbol and formula validation
    ↓
belief-aware planning
    ↓
OBTEA for optimal cost
or UHBTP/HBTP for anytime search
    ↓
causal execution-graph optimization
    ↓
resource/conflict validation
    ↓
LTLf and invariant checking
    ↓
compiled Program
    ↓
runtime execution + effect monitoring
```

Because `Location(mug)` is unknown, a belief-aware planner inserts `DetectObject`. Independent camera initialization and base preparation may be parallelized, but grasp and arm motion remain ordered by causal and resource dependencies.

During execution:

1. A perception event updates the belief.
2. Only dependent guards and planner monitors wake.
3. `Navigate` suspends on a ROS-style action future.
4. A safety monitor has a reserved execution budget and can abort lower-priority work.
5. An effect mismatch—for example, `Grasp` reports success but force sensing contradicts `Holding(mug)`—invalidates the declared outcome.
6. The runtime records grounding evidence and invokes local repair.
7. A new candidate is committed only at a safe boundary.
8. A finite abstraction can be exported to a formal backend for temporal checking.

Hard physical interlocks and certified low-level safety controllers should remain independent of the Go BT process. The BT provides mission-level safety structure and monitoring; it should not be treated as a replacement for hardware or lower-level certified protection.

For multiple robots, the planner additionally models:

* Agent ownership of actions.
* Shared resource reservations.
* Published intentions.
* Backup agents.
* Cross-tree dependencies.
* Communication-loss policy.
* Lease expiry.
* Idempotent recovery.

---

# 29. Benchmarking and acceptance criteria

BTPG’s evaluation approach is useful because ordinary metrics such as planning time and plan cost do not fully capture execution quality. The richer implementation should record planning progress, distance to the solvable region, and execution robustness in addition to timeout rate, cost, action count, tree size, and runtime ticks. ([IJCAI][11])

## Compatibility tests

Generate random legacy trees whose leaves record:

* Invocation order.
* Invocation count.
* Status.
* Error.
* Side effects.
* Panic behavior.

Compare:

```text
legacy execution
versus
LegacyExact adapter
```

for every external tick sequence.

## Optimized-runtime tests

For native nodes with complete contracts:

* Compare reference interpreter and flat executor.
* Mutate each declared dependency.
* Verify unaffected branches remain idle.
* Verify every affected observer wakes.
* Verify abort order.
* Verify transactional write behavior.
* Verify parallel conflict handling.

## Concurrency and lifecycle tests

Fuzz:

* Event order.
* Cancellation.
* Double completion.
* Stale futures.
* Simultaneous timer and external event.
* Patch during active action.
* Instance destruction.
* Resource release.
* Snapshot/restore.

Run the race detector over event producers and world schedulers.

## Determinism tests

Replay from:

```text
initial snapshot
event log
timer log
future completions
RNG seed/draw log
definition hash
program hash
```

and require identical status, write, and trace sequences.

## Serialization tests

* Canonical byte equality.
* Unknown-field preservation.
* Old-version migration.
* Malformed-size limits.
* Node/type/key ID collision handling.
* Cross-language conformance vectors.

## Performance tests

Measure:

* Compiled bytes per node.
* Mutable bytes per instance.
* Allocations per event.
* Quiescent check.
* One-variable fanout.
* Abort of active subtree.
* Thousands of instances sharing one program.
* Batch versus scalar handlers.
* Tracing overhead.
* Exact versus incremental mode.

---

# 30. Recommended implementation sequence

### Phase 1: model and exact interoperability

Implement:

* Immutable `Definition`.
* Typed `Type[C]` and `Key[T]`.
* Builder and validation.
* Stable IDs and canonical hashing.
* Metadata adapter.
* `AsNode`.
* `LegacyExact`.
* Reference interpreter.
* Differential compatibility suite.

This proves that the richer layer does not break the existing package.

### Phase 2: compiled flat runtime

Add:

* `Program`.
* `Instance`.
* Flat instructions.
* Typed state banks.
* Source maps.
* Deterministic traces.
* Snapshot/restore.
* Standard sequences, selectors, decorators, and actions.

### Phase 3: incremental/event-driven execution

Add:

* Typed variables/topics/futures.
* Reverse dependency indexes.
* Ready queues.
* Observer aborts.
* Timers.
* Transactional writes.
* `Incremental` and `VerifiedIncremental`.
* ECS/world batching.

### Phase 4: portable artifacts and hot reload

Add:

* Canonical codec.
* Semantic/artifact hashes.
* Capability manifests.
* Unknown-field preservation.
* Definition diff and patches.
* State migration.
* Security policy.
* Conformance vectors.

### Phase 5: planning and belief

Add:

* Formula and effect AST.
* Domain/problem/candidate API.
* PA-BT and BT Expansion reference planners.
* OBTEA.
* UHBTP heuristics.
* LLM `Advisor`.
* Belief backends.
* Observation and information-gathering actions.
* Active-inference leaf integration.
* Planner portfolio and anytime commit protocol.

### Phase 6: assurance and multi-agent operation

Add:

* Grounding evidence and effect monitors.
* LTLf mission monitors.
* Model-checking backends.
* Certificates and counterexample replay.
* Causal parallelization.
* Multi-agent resources, intentions, backups, and cross-tree planning.
* Statistical verification adapters.

---

# Final recommendation

The core architectural rule should be:

> **Model richly and immutably above the legacy ABI; compile into a shared flat event program; keep mutable state per runtime instance; expose the result as an ordinary legacy node; and treat old closures as opaque unless they voluntarily provide complete contracts.**

That produces all of the desired properties simultaneously:

* Existing `go-behaviortree` code remains valid.
* Rich node data is typed, versioned, and portable.
* Game workloads receive event-driven observers, flat storage, batching, LOD, hot reload, and deterministic replay.
* Robotics workloads receive symbolic planning, belief state, temporal goals, resource reasoning, grounding evidence, repair, and formal verification.
* Current and future planners remain plugins rather than forcing algorithm-specific concepts into the executor.
* Go 1.27 generic methods improve ergonomics without becoming a compatibility or interface dependency.
* Performance claims remain honest because opaque legacy behavior is never silently skipped.
* Formal guarantees remain honest because every certificate identifies its assumptions, semantic version, definition, domain, grounding, and verification profile.

[1]: https://github.com/joeycumines/go-behaviortree/blob/master/behaviortree.go "https://github.com/joeycumines/go-behaviortree/blob/master/behaviortree.go"
[2]: https://github.com/joeycumines/go-behaviortree/blob/master/metadata.go "https://github.com/joeycumines/go-behaviortree/blob/master/metadata.go"
[3]: https://github.com/joeycumines/go-behaviortree/blob/master/value.go "https://github.com/joeycumines/go-behaviortree/blob/master/value.go"
[4]: https://dev.epicgames.com/documentation/en-us/unreal-engine/behavior-tree-overview?application_version=4.27 "https://dev.epicgames.com/documentation/en-us/unreal-engine/behavior-tree-overview?application_version=4.27"
[5]: https://www.behaviortree.dev/docs/intro/ "https://www.behaviortree.dev/docs/intro/"
[6]: https://go.dev/doc/go1.27 "https://go.dev/doc/go1.27"
[7]: https://dev.epicgames.com/documentation/unreal-engine/behavior-tree-node-reference-in-unreal-engine?lang=en-US "https://dev.epicgames.com/documentation/unreal-engine/behavior-tree-node-reference-in-unreal-engine?lang=en-US"
[8]: https://arxiv.org/abs/1611.00230 "https://arxiv.org/abs/1611.00230"
[9]: https://ojs.aaai.org/index.php/AAAI/article/view/16755 "https://ojs.aaai.org/index.php/AAAI/article/view/16755"
[10]: https://www.ijcai.org/proceedings/2024/0755 "https://www.ijcai.org/proceedings/2024/0755"
[11]: https://www.ijcai.org/proceedings/2025/0969.pdf "https://www.ijcai.org/proceedings/2025/0969.pdf"
[12]: https://arxiv.org/abs/2406.00965 "https://arxiv.org/abs/2406.00965"
[13]: https://arxiv.org/abs/2008.09393 "https://arxiv.org/abs/2008.09393"
[14]: https://arxiv.org/abs/2011.09756 "https://arxiv.org/abs/2011.09756"
[15]: https://ojs.aaai.org/index.php/AAAI/article/view/40936 "https://ojs.aaai.org/index.php/AAAI/article/view/40936"
[16]: https://arxiv.org/abs/2208.05360 "https://arxiv.org/abs/2208.05360"
[17]: https://arxiv.org/abs/2502.11904 "https://arxiv.org/abs/2502.11904"
[18]: https://arxiv.org/abs/2307.06399 "https://arxiv.org/abs/2307.06399"
[19]: https://arxiv.org/abs/2101.01964 "https://arxiv.org/abs/2101.01964"
[20]: https://arxiv.org/abs/2502.18072 "https://arxiv.org/abs/2502.18072"
[21]: https://arxiv.org/html/2508.18820v1 "https://arxiv.org/html/2508.18820v1"
[22]: https://arxiv.org/abs/2512.04404v1 "https://arxiv.org/abs/2512.04404v1"
[23]: https://arxiv.org/abs/2411.14165 "https://arxiv.org/abs/2411.14165"
