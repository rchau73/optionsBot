# Documentation index

| Document | For |
|---|---|
| [../README.md](../README.md) | Technical overview: architecture, setup, configuration, testing |
| [../OptionStrategy.md](../OptionStrategy.md) | The trading strategy in plain language, with examples and risks |
| [architecture.md](architecture.md) | Detailed design: packages, concurrency, invariants, limitations |
| [operations.md](operations.md) | Running, monitoring, kill switch, troubleshooting |

## Diagrams

Every diagram has a Mermaid source (`.mmd`, edit this) and a rendered PNG (4× scale, zoom freely). Re-render with `make diagrams` (needs [`mmdc`](https://github.com/mermaid-js/mermaid-cli)).

### Structure

| Diagram | Shows | Source | Image |
|---|---|---|---|
| Architecture overview | packages, interfaces and how they connect | [mmd](arch_overview.mmd) | [png](arch_overview.png) |
| Data model | core types and their relationships | [mmd](data_model.mmd) | [png](data_model.png) |

### Sequences

| Diagram | Shows | Source | Image |
|---|---|---|---|
| Startup & reconcile | connect, subscribe, rebuild the book from Deribit, first entries | [mmd](seq_startup.mmd) | [png](seq_startup.png) |
| Entry order lifecycle | sizing, gating, submit, fill tracking, amend, timeout | [mmd](seq_entry.mmd) | [png](seq_entry.png) |
| Exit: stop-loss & rollout | rule priority, market vs IOC close, partial fills, who reopens | [mmd](seq_exit.mmd) | [png](seq_exit.png) |
| Gateway request & reconnect | queue, rate limit, breaker, reply routing, reconnect, fatal | [mmd](seq_gateway.mmd) | [png](seq_gateway.png) |
| GEX refresh & gating | open-interest gamma, flip, regime, leg shedding | [mmd](seq_gex.mmd) | [png](seq_gex.png) |
| Kill switch | cancel, flatten, retry, stay idle | [mmd](seq_kill_switch.mmd) | [png](seq_kill_switch.png) |

### Flows and state machines

| Diagram | Shows | Source | Image |
|---|---|---|---|
| Strategy event loop | one evaluation cycle | [mmd](strategy_eventloop.mmd) | [png](strategy_eventloop.png) |
| Rollout rules | decision tree in priority order | [mmd](strategy_rollout.mmd) | [png](strategy_rollout.png) |
| Position states | pending → open → closing → closed, partial fills | [mmd](strategy_position_states.mmd) | [png](strategy_position_states.png) |
| Market data | chain, subscriptions, push handling | [mmd](marketdata_flow.mmd) | [png](marketdata_flow.png) |
| Rate limiter | the two Deribit credit pools | [mmd](gateway_ratelimiter.mmd) | [png](gateway_ratelimiter.png) |
| Circuit breaker | what opens it, what bypasses it | [mmd](gateway_circuitbreaker.mmd) | [png](gateway_circuitbreaker.png) |
| Hedge report | when and what is written | [mmd](hedge_flow.mmd) | [png](hedge_flow.png) |
| Backtest loop | day loop, sweep, walk-forward | [mmd](backtest_flow.mmd) | [png](backtest_flow.png) |
| SimExecutor | backtest fill models | [mmd](backtest_simexec.mmd) | [png](backtest_simexec.png) |
