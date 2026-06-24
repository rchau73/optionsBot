---
title: "Orders — Component Detail"
---

## Orders Process Flow

```mermaid
flowchart TD
    subgraph EXECUTOR["Executor — live order placement"]
        A([Strategy\nOrder request]) --> B{Direction?}
        B -->|"Sell"| C[private/sell\nvia Gateway.Call]
        B -->|"Buy"| D[private/buy\nvia Gateway.Call]
        C --> E{TriggerReason\nstop-loss?\nkill-switch?\ngamma-close?}
        D --> E
        E -->|"Yes → PriorityHigh"| F[Priority Queue\nHigh channel]
        E -->|"No → PriorityLow"| G[Priority Queue\nLow channel]
        F --> H[Deribit API\nreturns order_id\navg_price\nfilled_amount]
        G --> H
        H --> I[Fill{orderID\nfillPrice\nqty\ntimestamp}]
    end

    subgraph STATE["StateManager — in-memory position tracking"]
        I --> J[AddPosition\nor update existing]
        J --> K[(positions map\nID → Position)]
        J --> L[(strangles map\nID → Strangle)]
        K --> M[NetGamma()\n= Σ γ×qty]
        K --> N[TotalNetDelta()\n= Σ δ×qty]
        K --> O[TotalMarginUsed()\n= Σ mid×qty]
    end

    subgraph LOGGER["OrderLogger — JSON audit trail"]
        I --> P[buildRecord\nFill + Position + Greeks]
        P --> Q[Compute derived fields\nintrinsic/extrinsic\nspread metrics]
        Q --> R{Closing\ntransaction?}
        R -->|"Yes"| S[Compute ROI fields\npnl · roi_pct · hold_days\ntheta_captured · roi_ann]
        R -->|"No"| T[Skip ROI fields\nomitempty → absent]
        S --> U[JSON marshal\nAppend to orders.log]
        T --> U
    end
```

## OrderLog Data Model

```mermaid
classDiagram
    class OrderLog {
        %% Core
        +time.Time Timestamp
        +string OrderID
        +string Instrument
        +string Direction
        +string OrderType
        +string TriggerReason
        +float64 Qty
        +float64 LimitPrice
        +float64 FillPrice
        +string Status
        %% Greeks snapshot
        +float64 Delta
        +float64 Gamma
        +float64 Theta
        +float64 Vega
        +float64 Rho
        +float64 IV
        +float64 IVPercentile
        %% Intrinsic / Extrinsic
        +float64 UnderlyingPrice
        +float64 Strike
        +float64 IntrinsicValue
        +float64 ExtrinsicValue
        +float64 IntrinsicPct
        +float64 ExtrinsicPct
        %% Spread quality
        +float64 Bid
        +float64 Ask
        +float64 Mid
        +float64 SpreadAbs
        +float64 SpreadPct
        +float64 FillVsMid
        %% ROI — closing only (omitempty)
        +float64 PremiumReceived
        +float64 CloseCost
        +float64 PnLUSD
        +float64 ROIPct
        +int HoldDays
        +float64 ThetaCapturedUSD
        +float64 ROIAnnualized
    }
```

## Derived Field Calculations

```mermaid
flowchart LR
    subgraph INTRINSIC["Intrinsic / Extrinsic"]
        A[option_type] --> B{Call or Put?}
        B -->|"Call"| C["intrinsic =\nmax(underlying − strike, 0)"]
        B -->|"Put"| D["intrinsic =\nmax(strike − underlying, 0)"]
        C --> E["extrinsic =\nfill_price − intrinsic"]
        D --> E
        E --> F["intrinsic_pct =\nintrinsic / fill_price × 100"]
        E --> G["extrinsic_pct =\nextrinsic / fill_price × 100"]
    end

    subgraph SPREAD["Spread Metrics"]
        H["spread_abs = ask − bid"]
        I["spread_pct = spread_abs / mid × 100"]
        J["fill_vs_mid = fill_price − mid"]
        I --> K{spread_pct >\nspreadAlertThreshold\n5%?}
        K -->|"Yes"| L["WARN wide spread alert"]
    end

    subgraph ROI["ROI Fields — closing only"]
        M["pnl_usd =\npremium_received − close_cost"]
        N["roi_pct =\npnl / premium × 100"]
        O["hold_days =\nround(exit − entry days)"]
        P["theta_captured ≈\ntheta_at_open × hold_days"]
        Q["roi_annualized =\nroi_pct / hold_days × 365"]
    end
```

## Trigger Reason Reference

```mermaid
graph LR
    subgraph TRIGGERS["TriggerReason values"]
        T1["entry\nInitial position open\nor reopen after rollout"]
        T2["rollout_19dte\nTime-based rollout\nDTE ≤ 19"]
        T3["rollout_delta_drift\nDelta fell below 0.10\nlegs rolled individually"]
        T4["rollout_roi\nROI ≥ 50% captured\nleg rolled individually"]
        T5["stop_loss_200pct\nLoss ≥ 200% of premium\nmarket close — emergency"]
        T6["gamma_close\nNet portfolio γ < 0\nmarket trend-based close"]
        T7["kill_switch\nSIGUSR1 or env var\nflatten everything"]
    end

    T5 --> R1["PriorityHigh\nQueue"]
    T6 --> R1
    T7 --> R1
    T1 --> R2["PriorityLow\nQueue"]
    T2 --> R2
    T3 --> R2
    T4 --> R2
```
