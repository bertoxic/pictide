**Pictide: investment-of-time assessment — 10 November 2026**

**Recommendation: do not fund a broad launch or an open-ended feature roadmap yet. Fund a small, paid validation experiment.** Pictide could become a viable focused business for Nigerian event photographers, but the current general gallery-plus-commission proposition has weak differentiation and unproven sales demand. There is currently insufficient evidence to call it a strong investment or a venture-scale opportunity. That is an assessment of today's evidence, not a claim that the business cannot succeed.

Scope: Nigeria; pre-revenue; event and graduation photographers initially, with weddings and other events also supported. Research comprises current primary vendor pages, a read-only source review, reproducible economic scenarios, and an independent challenge of the arithmetic and assumptions. No customers were interviewed, no competitor checkout was purchased/tested, and no production Pictide payment flow was run. Public competitor offers demonstrate competition, not their profitability. There is no defensible percentage probability of Pictide succeeding from this evidence.

**The strongest case for Pictide**

Photographers have real delivery and collection jobs: organize images, collect selections, share usable mobile previews, collect agreed payments, and deliver originals. Event photography also has an established retail-photo category internationally. GotPhoto, for example, advertises specialized volume-photography workflows at 12% on its free plan and 7–9% on paid plans, plus processing. This demonstrates that commission pricing can be accepted when attached to a selling workflow; it does not establish Nigerian buyer conversion. [GotPhoto pricing](https://www.gotphoto.com/pricing/)

Photographers can bring their own clients, so Pictide does not necessarily need to build an open marketplace or acquire every buyer through advertising. Existing software reduces the cost of running a pilot. The most promising hypothesis is to make participating photographers measurably more money or save enough time that they repeatedly pay for the service.

**The strongest case against substantial investment today**

Delivery usage and photo sales are different demand signals. A photographer can love free galleries while every recipient downloads for free. For example, Nigerian Kawa Studio lists a ₦350,000 wedding package that already includes 200+ edited images and an online gallery/download. That package is evidence of bundled delivery; its whole price must not be counted as Pictide's separately sold-photo volume. This is one illustrative studio, not a national survey. [Kawa Studio rate card](https://www.kawagraphy.com/pricing)

The broad product also competes with established international tools, Nigerian services, and the existing workflow of messaging, bank transfer and shared folders. A new product must overcome switching friction and earn trust with irreplaceable images and money. QR sharing, watermarking, galleries and naira payments alone are unlikely to provide durable differentiation.

| Alternative | Published offer checked | Implication |
|---|---|---|
| [Pixieset](https://pixieset.com/pricing/) | Free 3GB with 15% store commission; Basic 10GB is $8/month billed annually or $10 monthly, with no platform sales commission. Payment processing remains separate. | A mature general gallery benchmark; competing on core features alone is difficult. |
| [Imvoy](https://useimvoy.com/) | Nigerian public beta; free 2GB/3 galleries; Professional ₦4,000/month/20GB. FAQ describes manual direct-bank-transfer confirmation and 0% commission. | Local pricing and payment collection workflows already exist; manual confirmation is a distinction from automated checkout. |
| [SnapStaq](https://snapstaq.com/) | Core ₦3,500/month/50GB; Advanced ₦7,500/150GB. Website currently advertises a temporary all-features promotion until 30 March 2027. | Delivery can be purchased at a modest fixed monthly price. |
| [Paytograph](https://paytograph.com/) | Payment-gated delivery; 6% per collection, minimum ₦350, maximum ₦10,000; minimum project ₦5,000. | Direct competition for collecting existing client balances. All-in processor treatment was not independently verified. |
| [Picha](https://www.picha.fun/about) | Nigerian event galleries with QR/link delivery, selfie-based photo discovery and Paystack booking deposits. | Even event discovery and local-payment positioning are already advertised locally. |

These products are not feature-identical. Their prices are evidence of alternatives, not proof that Pictide must match the cheapest one. At 10%, however, Pictide's fee reaches ₦4,000 on just ₦40,000 monthly sales. To retain successful sellers at higher volumes it needs incremental sales value, a cap, a paid tier, or another compelling benefit.

**Which events make the most sense**

| Segment | Revenue hypothesis | What must be proven |
|---|---|---|
| Graduation portraits and events with existing individual-photo purchases | Commission on genuine image/package sales | Graduates pay through the gallery; photographers repeat; organizers permit practical distribution. |
| Organized sports, recurring portraits and similar volume work | Repeat sales workflow and bundles | Accessible photographer/organizer relationships, efficient photo discovery, sufficient buyer spending. |
| Weddings, corporate events and host-paid celebrations | Paid delivery/workflow plan or event fee; optional extra sales | Photographer or host pays for delivery value; do not assume guests buy images already included in coverage. |

These are hypotheses rather than validated segment rankings. For sales, begin with photographers who can show previous receipts for selling individual images. Graduation is seasonal: a successful graduation month does not establish year-round recurring revenue. The app can support many event types while acquisition and measurement initially focus on one.

**Simulation: what has to happen financially**

The accompanying `simulate.py` uses only Python's standard library and writes `results.json`. These are deterministic scenarios, not fitted forecasts, success probabilities, or measured conversion rates. No arbitrary Monte Carlo probabilities are presented as evidence.

GMV means money paid for photos through Pictide. It excludes free downloads, all photographer income earned elsewhere and balances paid outside the platform. An active photographer means an account being served during the month, including accounts with only free delivery. A selling photographer makes at least one paid photo sale that month.

Published inputs: Paystack's standard Nigerian local fee is 1.5% + ₦100, with ₦100 waived below ₦2,500 and the fee capped at ₦2,000. Cloudflare R2 Standard storage is $0.015/GB-month, with no direct egress fee; requests and other services can still cost money. [Paystack pricing](https://paystack.com/pricing), [R2 pricing](https://developers.cloudflare.com/r2/pricing/)

All other inputs are assumptions:

| Shared assumption | Value and meaning |
|---|---|
| Pictide commission | 10%, a proposed test price; current code default is 0%. |
| Payment fee bearer | Pictide absorbs processing in the main scenarios. |
| Nonrecoverable economic losses | 1% of GMV; an allowance, not a measured refund or fraud rate. Recoverable refunded photographer proceeds must not be double-counted as platform loss. |
| R2 retained storage | 20GB per active account × $0.015 × illustrative ₦1,500/$ = ₦450/month. FX is a modelling input, not a current exchange-rate quote. |
| Other variable technology | ₦100/active/month; placeholder for previews, requests, compute and similar costs, not a measured cloud invoice. |
| Support capacity | 6 minutes/active/month at ₦3,000/hour = ₦300. This budgets incremental support capacity separately from founder product/business work. |
| Acquisition cost | ₦10,000 for an activated photographer, including costed outreach/onboarding. |
| Monthly churn | 5%; steady-state replacement acquisition therefore costs ₦500/active/month. |
| Fixed monthly costs | ₦750,000, including ₦500,000 target founder compensation and ₦250,000 overhead. These are adjustable targets, not observed spending. |

Service cost totals ₦850/active/month; with replacement acquisition, total non-transaction variable cost is ₦1,350. Costs exclude taxes, initial development and any separate payout/levy costs not absorbed in the allowance. Storage of old, inactive accounts must be added if retained; this model does not automatically grow that backlog. Real MongoDB and image-processing costs may exceed the placeholders. Each scenario assumes uniform order sizes; production fees need to be summed order by order because of the threshold and cap.

| Monthly result with 100 active photographers | Downside | Working hypothesis | Upside |
|---|---:|---:|---:|
| Photographers making sales | 20% | 40% | 60% |
| Sales per selling photographer | ₦25,000 | ₦100,000 | ₦250,000 |
| Average order | ₦2,500 | ₦5,000 | ₦5,000 |
| Total photo GMV | ₦500,000 | ₦4,000,000 | ₦15,000,000 |
| Pictide commission revenue | ₦50,000 | ₦400,000 | ₦1,500,000 |
| Processing fees | ₦27,500 | ₦140,000 | ₦525,000 |
| Assumed nonrecoverable losses | ₦5,000 | ₦40,000 | ₦150,000 |
| Service cost | ₦85,000 | ₦85,000 | ₦85,000 |
| Replacement acquisition | ₦50,000 | ₦50,000 | ₦50,000 |
| Contribution before fixed cost | **−₦117,500** | **₦85,000** | **₦690,000** |
| Result after fixed cost and founder pay, before tax | **−₦867,500** | **−₦665,000** | **−₦60,000** |
| Steady-state active accounts to break even | None at these economics | **883** | **109** |

The working hypothesis requires each seller to complete 20 orders of ₦5,000 monthly. The upside requires 50 such orders per seller and a larger proportion of selling accounts. These are substantial recurring sales requirements. They are not achieved by having that many gallery visitors.

At a ₦5,000 order, a 10% commission produces ₦500 revenue, processing takes ₦175, and the assumed loss allowance takes ₦50: ₦275 remains before service, acquisition and fixed costs. The main working-case contribution is only ₦850 per active account monthly. At 1,000 active accounts it yields approximately ₦100,000/month after the specified fixed costs and founder pay, before tax. This is compatible with a small business, but it leaves little room for estimation error.

Sensitivity materially changes the answer:

- If photographers bear processing, the working break-even becomes 334 active accounts. At ₦5,000/order, their combined platform-plus-processing charge becomes 13.5%; retaining the same sales and adoption is not guaranteed. Paystack supports explicit fee-bearer and split-payment arrangements, but those are not yet wired into the inspected Pictide flow. [Paystack split-payment documentation](https://paystack.com/docs/payments/split-payments/)
- Reducing Pictide's commission from 10% to 6%, while retaining the working sales/cost assumptions and absorbing processing, makes contribution negative at −₦750 per active. More accounts then increase losses.
- Raising retained R2 bytes from 20GB to 50GB/active pushes working break-even to 4,286 active accounts; 100GB makes contribution negative. This holds all other costs fixed and may understate associated database/compute growth.
- At 5% monthly churn, approximately 54% of a starting cohort remains after 12 months. This is an assumption that must be measured using event eligibility and seasonality, not inferred from dormant logins.
- Higher prices, fee pass-through and storage limits can change buyer/seller behavior. This arithmetic holds demand constant when changing a price; it does not model price elasticity.

**Growth requires more capital than steady-state break-even suggests**

A separate 12-month scenario starts at zero and acquires 50 new active photographers each month at ₦10,000 each, with 5% monthly churn. Each new account immediately earns a full month's scenario economics, an optimistic ramp assumption. Gross acquisition costs replace the steady-state acquisition allowance rather than being added twice.

Under working sales behavior it ends at approximately 460 active accounts and ₦18.39 million monthly GMV, yet loses approximately **₦10.59 million over the year**, including ₦6 million founder compensation. Excluding that founder pay would reduce the modeled cash requirement to about ₦4.59 million while leaving the time investment unpaid. Under upside behavior, the same acquisition trajectory earns approximately ₦9.17 million over the year after that founder pay. Those divergent results show why measured sales intensity matters more than confidence in a single projection. Neither path establishes that acquiring 50 suitable photographers monthly is possible.

**Current code: commercial launch gates**

The source review supports treating the software as a starting asset and an unverified commerce prototype:

- Default commission is zero: [config.go](</C:/Users/Hp/Desktop/lumen v2/internal/config/config.go:66>). Paid checkout computes the configured fee: [commerce.go](</C:/Users/Hp/Desktop/lumen v2/internal/httpapi/commerce.go:119>).
- Payment initialization does not pass a split/subaccount or fee bearer: [paystack.go](</C:/Users/Hp/Desktop/lumen v2/internal/paystack/paystack.go:68>). Wallet earnings deduct platform commission but not processor costs, and payout configuration reports false: [wallet.go](</C:/Users/Hp/Desktop/lumen v2/internal/httpapi/wallet.go:28>). Transfer webhooks return 503: [commerce.go](</C:/Users/Hp/Desktop/lumen v2/internal/httpapi/commerce.go:236>). A ₦10,000 order with zero commission can therefore record ₦10,000 owed while the standard processor settles ₦9,750. Displayed wallet earnings are not reconciled cash.
- The README states paid checkout, verification and downloads remain untested in the MongoDB conversion and payouts are not implemented: [README.md](</C:/Users/Hp/Desktop/lumen v2/README.md:5>). Test files exist, including opt-in integration tests, but their presence does not establish successful production payments. This review did not run them.
- Direct uploads retain an original and a full-resolution PNG master in R2, plus four variants in MongoDB: [direct_upload.go](</C:/Users/Hp/Desktop/lumen v2/internal/httpapi/direct_upload.go:184>). The PNG can be larger than the uploaded JPEG; measure actual byte ratios. Archive operations do not reclaim the stored image bytes: [photos.go](</C:/Users/Hp/Desktop/lumen v2/internal/httpapi/photos.go:183>), [galleries.go](</C:/Users/Hp/Desktop/lumen v2/internal/mongostore/galleries.go:72>). No enforced studio storage quota was found in inspected handlers.
- Preview serving goes through the application/GridFS. R2's free egress does not eliminate database delivery, image-processing, request, compute or support expenses. Temporary ZIP cleanup also needs a configured lifecycle rule.
- Sales summaries exist, but the review found no complete visitor-to-purchase funnel or cohort-retention instrumentation. Public gallery links also need explicit privacy/access expectations. This was not a comprehensive security audit.

Before collecting meaningful public sales: verify real payment → entitlement → original download → reconciled photographer settlement, retry/idempotency behavior and refunds; choose who bears each fee; and enforce storage/expiry rules. Logging a balance is not a substitute for completing a payout. Avoid marketing acquisition at scale before these are reliable.

**A bounded test that can change the recommendation**

Suggested commitment: six to eight weeks, up to 120 founder hours and ₦200,000 external cash, subject to actual affordability. This is a proposed cap, not an estimate that every production fix fits inside it. If essential payment safety/reliability cannot fit, reassess before widening the spend.

1. Interview 15 relevant photographers about their last three events, actual individual-photo receipts, current delivery costs and frequency. Recruit 12–20 with upcoming sales-eligible events, including at least half outside the founder's close circle. Separate weddings with prepaid delivery from events with real photo retail.
2. Run approximately 30 eligible galleries and aim for at least 100 genuine paid orders. Charge the intended fee from the beginning, with any subsidy disclosed and excluded from willingness-to-pay conclusions. These sample sizes are learning targets, not statistically conclusive thresholds.
3. Measure event attendance where available, gallery visitors, find-photo success, purchasers, average order, GMV, all payment fees/losses, retained bytes, support minutes, acquisition effort and repeat events. Distinguish spending shifted from existing offline sales from incremental photographer income.
4. Continue only if payments and payouts reconcile; a majority of eligible photographers choose Pictide again for their next suitable event at the real price; service contribution is positive after measured costs; and a repeatable acquisition path offers a payback period compatible with the available runway. Target six months or less for a cash-constrained launch, while recognizing that short pilots cannot establish lifetime value.
5. Recalculate the break-even count using measured cohorts. If it requires thousands of active accounts but the practical channel reaches dozens, do not proceed on hoped-for scale. If demand is for delivery but guests do not buy, test payment for delivery; if photographers will not pay for either outcome, stop further business investment.

For wedding and general delivery, a limited trial followed by a storage-capped subscription or per-event charge is better aligned with the service cost. ₦3,500–₦5,000/month is a reasonable local price range to test, not validated Pictide willingness to pay. Do not immediately build a large subscription system: first secure actual paid pilots. For event sales, test bundles and a transparent commission or hybrid/capped plan. Organizer relationships and reliable repeat use are more valuable early evidence than extra generic features.

**Investment decision**

Pictide's current state is a poor basis for a large commitment of money or another open-ended development period. The combination of free delivery costs, commission leakage, unproven guest purchasing, competition and unfinished settlement is material. A small controlled experiment remains rational because there is a credible customer problem and existing code to test it. The evidence that would justify investing more is paid, repeated usage with positive measured economics and a reachable acquisition channel. Existing development time is a sunk cost; the next allocation should depend on that evidence.
