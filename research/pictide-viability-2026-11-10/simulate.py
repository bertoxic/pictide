"""Pictide scenario arithmetic; assumptions, not a demand forecast.

Run: python simulate.py
Outputs results.json beside this file. Python standard library only.
All monetary values are NGN unless explicitly USD. No network or app writes.
"""
from dataclasses import dataclass, asdict, replace
from pathlib import Path
import json
import math


@dataclass(frozen=True)
class Assumptions:
    active: int = 100
    seller_share: float = 0.40
    monthly_gmv_per_seller: float = 100_000
    order_value: float = 5_000
    commission: float = 0.10
    platform_pays_processing: bool = True
    loss_rate: float = 0.01
    retained_r2_gb_per_active: float = 20
    r2_usd_per_gb: float = 0.015
    illustrative_ngn_per_usd: float = 1_500
    other_variable_tech_per_active: float = 100
    support_minutes_per_active: float = 6
    support_hourly_cost: float = 3_000
    acquisition_cost_per_active: float = 10_000
    monthly_churn: float = 0.05
    fixed_cost_including_founder_pay: float = 750_000


def processor_fee(amount):
    """Paystack published local standard fee, not international/other products."""
    return min(2_000, 0.015 * amount + (100 if amount >= 2_500 else 0))


def economics(a):
    # Uniform order values: actual fee mix must be calculated order by order.
    gmv = a.active * a.seller_share * a.monthly_gmv_per_seller
    orders = gmv / a.order_value
    gross_revenue = gmv * a.commission
    provider_fees = orders * processor_fee(a.order_value)
    platform_provider_cost = provider_fees if a.platform_pays_processing else 0
    nonrecoverable_losses = gmv * a.loss_rate
    storage_per_active = (a.retained_r2_gb_per_active * a.r2_usd_per_gb
                          * a.illustrative_ngn_per_usd)
    support_per_active = a.support_minutes_per_active / 60 * a.support_hourly_cost
    service_cost_per_active = (storage_per_active
                              + a.other_variable_tech_per_active + support_per_active)
    service_cost = a.active * service_cost_per_active
    replacement_acquisition = a.active * a.monthly_churn * a.acquisition_cost_per_active
    contribution_before_acquisition = (gross_revenue - platform_provider_cost
                                       - nonrecoverable_losses - service_cost)
    contribution = contribution_before_acquisition - replacement_acquisition
    contribution_per_active = contribution / a.active
    break_even = (math.ceil(a.fixed_cost_including_founder_pay / contribution_per_active)
                  if contribution_per_active > 0 else None)
    return {
        "assumptions": asdict(a),
        "gmv": gmv,
        "orders": orders,
        "platform_revenue": gross_revenue,
        "provider_fees": provider_fees,
        "platform_provider_cost": platform_provider_cost,
        "nonrecoverable_loss_allowance": nonrecoverable_losses,
        "storage_cost_per_active": storage_per_active,
        "service_cost_per_active": service_cost_per_active,
        "service_cost": service_cost,
        "replacement_acquisition": replacement_acquisition,
        "contribution_before_acquisition": contribution_before_acquisition,
        "contribution_after_replacement_acquisition": contribution,
        "contribution_per_active": contribution_per_active,
        "monthly_result_after_founder_pay_before_tax": (
            contribution - a.fixed_cost_including_founder_pay),
        "break_even_active": break_even,
    }


def growth(a, new_active_per_month=50, months=12):
    # New accounts activate at month start and earn a full month immediately.
    # This is optimistic about ramp time. Charge actual gross acquisition;
    # do not ALSO charge the steady-state replacement allowance.
    active = 0
    cumulative = 0
    rows = []
    for month in range(1, months + 1):
        active = active * (1 - a.monthly_churn) + new_active_per_month
        e = economics(replace(a, active=active))
        result = (e["contribution_before_acquisition"]
                  - new_active_per_month * a.acquisition_cost_per_active
                  - a.fixed_cost_including_founder_pay)
        cumulative += result
        rows.append({"month": month, "active": active, "gmv": e["gmv"],
                     "monthly_result": result, "cumulative_result": cumulative})
    return rows


def main():
    working = Assumptions()
    scenarios = {
        "downside": replace(working, seller_share=0.20,
                            monthly_gmv_per_seller=25_000, order_value=2_500),
        "working_hypothesis": working,
        "upside": replace(working, seller_share=0.60, monthly_gmv_per_seller=250_000),
    }
    results = {
        "scope": "Nigeria, pre-revenue, event/graduation sales and broader event delivery",
        "research_date": "2026-11-10",
        "warning": "Illustrative scenarios; no measured demand probabilities or forecast.",
        "scenarios": {k: economics(v) for k, v in scenarios.items()},
        "seller_bears_processing": {
            k: economics(replace(v, platform_pays_processing=False))
            for k, v in scenarios.items()},
        "working_commission_sensitivity": {
            str(rate): economics(replace(working, commission=rate))
            for rate in [0, 0.06, 0.08, 0.10, 0.12, 0.15]},
        "working_storage_sensitivity": {
            str(gb): economics(replace(working, retained_r2_gb_per_active=gb))
            for gb in [5, 20, 50, 100]},
        "growth_50_new_active_per_month": {
            k: growth(v) for k, v in scenarios.items()},
        "working_acquisition_sensitivity": {
            str(cac): economics(replace(working, acquisition_cost_per_active=cac))
            for cac in [0, 5_000, 10_000, 20_000, 30_000]},
    }
    # Check formula boundaries that materially affect conclusions.
    assert processor_fee(2_000) == 30
    assert processor_fee(2_500) == 137.5
    assert processor_fee(5_000) == 175
    assert processor_fee(200_000) == 2_000
    assert results["scenarios"]["working_hypothesis"]["break_even_active"] == 883
    assert results["scenarios"]["upside"]["break_even_active"] == 109
    assert results["scenarios"]["downside"]["break_even_active"] is None
    target = Path(__file__).with_name("results.json")
    target.write_text(json.dumps(results, indent=2), encoding="utf-8")
    for name, e in results["scenarios"].items():
        print(name, {k: e[k] for k in ["gmv", "platform_revenue", "provider_fees",
              "contribution_per_active", "monthly_result_after_founder_pay_before_tax",
              "break_even_active"]})
        print("  month12", results["growth_50_new_active_per_month"][name][-1])
    print("fee_sensitivity", {k: (v["contribution_per_active"], v["break_even_active"])
          for k, v in results["working_commission_sensitivity"].items()})
    print("storage_sensitivity", {k: (v["contribution_per_active"], v["break_even_active"])
          for k, v in results["working_storage_sensitivity"].items()})
    print("Wrote", target)


if __name__ == "__main__":
    main()
