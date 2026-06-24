---
title: "Strategy — Component Detail"
---

## Main Event Loop

```mermaid
flowchart TD
    A([Bot Start]) --> B[openStrangles\ninitial entry]
    B --> C{Event loop\nselect}

    C -->|"ctx.Done"| D[return ctx.Err]
    C -->|"killSwitchCh"| E[killSwitch\nclose ALL at market\nTrigger=kill_switch]
    C -->|"Tick from MarketData"| F[onTick]

    subgraph ON_TICK["onTick()"]
        F --> G[updatePositions\nmatch instrument → update Mid+Greeks]
        G --> H[gamma.PushPrice\nunderlyingPrice]
        H --> I{gamma.Evaluate\nnet γ?}
        I -->|"γ ≥ 0 → normal"| J[Evaluate all\nopen positions]
        I -->|"γ < 0 + bear trend"| K[Close all PUT legs\nat market\nTrigger=gamma_close]
        I -->|"γ < 0 + bull trend"| L[Close all CALL legs\nat market\nTrigger=gamma_close]
        K --> M[maybeOpenStrangles\nafter gamma normalises]
        L --> M
        J --> N{Each position:\nEvaluateLeg}
        N --> O{Decision?}
        O -->|"None"| N
        O -->|"Any action"| P[handleRollout]
        P --> M
        M --> Q[hedgeRpt.MaybeReport\nif delta changed >5%]
    end
```

## Rollout Decision — Priority Order

```mermaid
flowchart TD
    A([Position]) --> B{LossPct ≥\nstopLossMultiplier\ndefault 2.0}
    B -->|"Yes"| C[ActionStopLoss\nTrigger=stop_loss_200pct\nMarket close immediate]
    B -->|"No"| D{DTE ≤\nrolloutDTE\ndefault 19}
    D -->|"Yes"| E[ActionRollNextMonth\nTrigger=rollout_19dte\nClose whole strangle\nReopen next monthly]
    D -->|"No"| F{absδ <\ndeltaDriftThreshold\n0.10 AND DTE ≥ 25}
    F -->|"Yes"| G[ActionRollSameLeg\nTrigger=rollout_delta_drift\nClose this leg only\nReopen δ=0.16 ≥25 DTE]
    F -->|"No"| H{ROIPct ≥\nroiTakeProfit\n0.50 AND DTE ≥ 25}
    H -->|"Yes"| I[ActionRollSameLeg\nTrigger=rollout_roi\nClose this leg only\nReopen δ=0.16 ≥25 DTE]
    H -->|"No"| J[ActionNone\nkeep holding]

    style C fill:#ffcdd2,stroke:#c62828
    style E fill:#fff9c4,stroke:#f9a825
    style G fill:#e8f5e9,stroke:#2e7d32
    style I fill:#e3f2fd,stroke:#1565c0
    style J fill:#f5f5f5,stroke:#9e9e9e
```

## Rollout Execution Flow

```mermaid
flowchart LR
    A{Action} -->|"StopLoss"| B[Submit market BUY\nimmediate fill\nLog close]
    A -->|"RollNextMonth"| C{Limit order\nor fallback?}
    C -->|"First: limit at mid"| D[Submit limit BUY\nwait orderFillTimeoutSec]
    C -->|"Timeout: market"| E[Submit market BUY]
    D --> F[Log close]
    E --> F
    F --> G[Remove from state]
    G --> H{Action was\nRollNextMonth?}
    H -->|"Yes"| I[NextMonthlyExpiry\nSelectStrike δ=0.16\nSell limit → new leg]
    H -->|"No (RollSameLeg)"| J{DTE ≥ 25\nexpiry available?}
    J -->|"Yes"| K[SelectStrike δ=0.16\nnext avail expiry ≥25 DTE\nSell limit → new leg]
    J -->|"No"| L[Log warning\nskip reopen]
    A -->|"StopLoss AND solo leg remains"| M{Solo leg\ndecision?}
    M -->|"RollNextMonth"| N[Close solo + open\nfull fresh strangle\nper entry rules]
    M -->|"Other"| O[Apply normal\nrollout rules]
```

## Entry Logic — Strike & Expiry Selection

```mermaid
flowchart TD
    A([openStrangles\ncalled at startup\n+ maybeOpen each tick]) --> B[AllInstruments\nfrom MarketData]
    B --> C[AccountEquity\nfrom Executor]
    C --> D[IVPercentile\nfrom DVOLTracker]
    D --> E{For each\ntargetDTE\n15·20·30·45}

    E --> F[SelectExpiry\nfind nearest expiry in\ntargetDTE ± maxDTEDeviation\nprefer lower DTE]
    F --> G{Expiry\nfound?}
    G -->|"No"| H[Log skip\nnext targetDTE]
    G -->|"Yes"| I[SelectStrike CALL\nOTM filter absδ < 0.5\nMid > 0\nSort by ‖δ‖ − 0.16‖\npick closest]
    I --> J[SelectStrike PUT\nsame logic\nneg delta]
    J --> K[quantizeAmount\nbudget/numDTEs\nstep = max(cfg.MinTA,\ninst.MinTradeAmount)]
    K --> L{marginGuard\nWithinLimit?}
    L -->|"No → skip"| H
    L -->|"Yes"| M[openStrangle\nexchStep re-quantize\nSubmit sell call limit\nSubmit sell put limit]
    M --> N[Pending Strangle\nawait fills]
    N --> O{Both legs\nfilled?}
    O -->|"Yes"| P[activateStrangle\nAdd to State\nLog open both legs]
    O -->|"Timeout"| Q[Cancel unfilled leg\ntry market fill]
```

## Gamma Monitor

```mermaid
flowchart TD
    A[Each tick:\nPushPrice underlyingPrice] --> B[Trim history\n> lookbackDays+1]
    B --> C[Calculate\nnet gamma\n= Σ γ×qty across\nall open positions]
    C --> D{net γ < 0?}
    D -->|"No → normal"| E[GammaActionNone]
    D -->|"Yes"| F[detectTrend\ncompare latest vs\n1-day-ago price]
    F --> G{Trend?}
    G -->|"latest < oldest\n→ bear"| H[GammaActionClosePuts\nClose all short puts\nat market]
    G -->|"latest > oldest\n→ bull"| I[GammaActionCloseCalls\nClose all short calls\nat market]
    G -->|"unchanged\n→ no data"| E
    H --> J[Log gamma_trigger\ntimestamp·gamma·direction]
    I --> J
    J --> K[Re-establish\nfull strangles\nper entry rules]
```

## Position Lifecycle — State Diagram

```mermaid
stateDiagram-v2
    [*] --> PendingFill : Submit sell limit order\n(entry or reopen)

    PendingFill --> Active : Fill confirmed\n(limit filled or market fallback)
    PendingFill --> Cancelled : orderFillTimeoutSec elapsed\n→ cancel + market retry

    Active --> Active : Tick update\nMid / Greeks refreshed

    Active --> RollingOut : EvaluateLeg returns\nActionRollNextMonth\nActionRollSameLeg

    Active --> EmergencyClose : EvaluateLeg returns\nActionStopLoss\n(loss ≥ 200%)

    Active --> GammaClose : GammaMonitor triggers\n(net γ < 0)

    Active --> KillSwitch : SIGUSR1 received\nor kill_switch env

    RollingOut --> Closed : Buy limit submitted\n(fill or 1-day timeout\n→ market fallback)
    EmergencyClose --> Closed : Buy market fills\nimmediately
    GammaClose --> Closed : Buy market fills\nimmediately
    KillSwitch --> Closed : Buy market fills\nimmediately

    Closed --> [*] : Removed from state\nROI fields logged\nto orders.log

    note right of Active
        Monitored every tick:
        • DTE countdown
        • delta drift check
        • ROI% check
        • loss% check
    end note

    note right of RollingOut
        Limit order active.
        Price amended if market
        drifts > orderSlippagePct
        (max orderMaxAdjustments)
    end note
```

## Margin Guard — IV Risk Model

```mermaid
flowchart LR
    A[ivPercentile] --> B{Band}
    B -->|"≥ 70"| C[allowedMargin\n= equity × 0.35]
    B -->|"30–69"| D[allowedMargin\n= equity × 0.25]
    B -->|"< 30"| E[allowedMargin\n= equity × 0.15]
    C --> F{currentMargin\n+ newCost\n≤ allowed?}
    D --> F
    E --> F
    F -->|"Yes"| G[Allow entry]
    F -->|"No"| H[Skip strangle\nfor this DTE\nlog margin limit]
```
