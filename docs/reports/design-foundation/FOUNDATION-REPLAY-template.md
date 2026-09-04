# Foundation replay record — {project}

> 状态：open / observed / concluded
> 日期：YYYY-MM-DD
> Foundation：docs/design/DESIGN.md@vX.Y.Z / missing / N/A
> 对照协议：blueprint/L6-design-foundation-replay.md

Fill this after a real UI project has used Project Design Foundation across
at least two `UI impact=changed` REQs. Do not fill it in the template factory.

## 1. Context

| 项 | 记录 |
|:---|:---|
| Product / surface | |
| REQs observed | REQ-… |
| Humans who confirmed F2 / F3 / F6 | |
| UI Lab present? | yes / no |

## 2. Signals (L4 §11.4)

| Signal | Before Foundation | After Foundation | Evidence (REQ / chat / PR) |
|:---|:---|:---|:---|
| Repeated style questions (“what color / what style”) | | | |
| Agent can explain why a page is organized this way | | | |
| Default UI / one-off styles | | | |
| Human only reviews macro direction and exceptions | | | |
| Same-semantic components rewritten across modules | | | |
| Surface Profile explains consumer vs operations | | | |
| Local findings return via CP-* / Grammar revision | | | |

## 3. Skip / fail events

| Date | Event | Was it a prompt miss, missing template, or a need for a mechanical gate? |
|:---|:---|:---|
| | Agent locked a changed REQ with no DESIGN.md | |
| | Agent asked the human to pick a hex | |
| | Third primary button appeared | |
| | Snapshot passed but users rejected the direction | |

## 4. Gate promotion decision

Promote an advisory check to `--strict` CI or a Runtime hint **only** when
the same skip repeats and a document/prompt fix already failed. Aesthetic
quality stays out of fail-closed gates.

| Candidate check | Observed stable failure? | Promote? | Why / why not |
|:---|:---|:---|:---|
| DESIGN.md exists + published for changed REQ | | no / ready-hint / strict CI | |
| Foundation reference + Derivation Note path | | | |
| Unregistered hex lint | | | |
| Duplicate component names | | | |
| Golden Screen snapshot drift | | | |

## 5. Conclusion

{Did Foundation reduce cross-module visual rework and low-information style questions? What should change in L4/L5?}
