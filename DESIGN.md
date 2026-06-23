# AI·로봇·헬스케어 행사 모아보기 Design System

## 1. Atmosphere & Identity

A quiet event finder for people comparing upcoming COEX/KINTEX events. The interface should feel factual, restrained, and fast rather than promotional. The signature is a warm paper-like base with teal operational accents and compact event cards that prioritize dates, venue, category, and source clarity over decoration.

## 2. Color

### Palette

| Role | Token | Light | Dark | Usage |
|------|-------|-------|------|-------|
| Background | `--bg` | `#faf8f4` | `#1d1b18` | Page background |
| Surface/primary | `--surface` | `#ffffff` | `#26231f` | Header, filters, cards, modal |
| Surface/secondary | `--surface-2` | `#f2efe8` | `#2e2a25` | Skeleton shimmer, muted panels |
| Text/primary | `--ink` | `#1a1815` | `#ede8df` | Main copy and headings |
| Text/secondary | `--ink-2` | `#5a554d` | `#b0aa9e` | Descriptions and metadata |
| Text/tertiary | `--ink-3` | `#8a8478` | `#8a8478` | Labels, disabled text, source meta |
| Border/default | `--rule` | `#e3ded3` | `#3a352e` | Dividers, controls, cards |
| Accent/primary | `--accent` | `#0d5563` | `#5fb0c2` | Links, focus, hover borders |
| Accent/hover | `--accent-2` | `#0a3f4a` | `#4a8a9a` | Stronger accent states |
| Badge/background | `--badge-bg` | `#f0ece2` | `#2e2a25` | Neutral badges and gaps |
| Category/AI | `--chip-ai` / `--chip-ai-bg` | `#c4604a` / `#f4e6df` | `#e08a72` / `#3a2a24` | AI category chip |
| Category/robotics | `--chip-robotics` / `--chip-robotics-bg` | `#4a6a8a` / `#e2e8f0` | `#8aa8c8` / `#2a3540` | Robotics category chip |
| Category/bio | `--chip-bio` / `--chip-bio-bg` | `#6a7d4a` / `#e8eedd` | `#a2b88a` / `#2e3a28` | Bio category chip |
| Category/medical | `--chip-med` / `--chip-med-bg` | `#8a5a3a` / `#f0e4d6` | `#c89070` / `#3a2e22` | Medical-device category chip |
| Category/health | `--chip-health` / `--chip-health-bg` | `#7a4a6a` / `#ecdde6` | `#b888a8` / `#352a35` | Digital-health category chip |
| Status/error | `--status-cancelled` / `--status-cancelled-bg` | `#a83232` / `#f4d9d9` | `#e88080` / `#3a2020` | Error and cancelled states |
| Overlay | `--modal-overlay` | `rgba(26, 24, 21, 0.55)` | `rgba(0, 0, 0, 0.72)` | Modal backdrop |

### Rules

- Accent is reserved for links, focus outlines, and interactive hover states.
- Category and status colors must be semantic chips, never decorative page color.
- Add any new semantic color here before using it in `static/index.html`.

## 3. Typography

### Scale

| Level | Size | Weight | Line Height | Tracking | Usage |
|-------|------|--------|-------------|----------|-------|
| Page title | `22px` | 600 | 1.35 | 0 | Product title |
| Modal title | `19px` | 600 | 1.35 | 0 | Event detail title |
| Card title | `15.5px` | 600 | 1.4 | 0 | Event card name |
| Body | `15px` | 400 | 1.55 | 0 | Default page text |
| Body/sm | `13px-14px` | 400 | 1.5 | 0 | Metadata, summaries, links |
| Control | `13.5px` | 400 | 1.4 | 0 | Inputs and buttons |
| Caption | `12px-12.5px` | 500 | 1.4 | 0.02em | Filter labels, footer, source meta |
| Chip | `11px` | 500 | 1.4 | 0.01em | Category/status/cost badges |

### Font Stack

- Primary: `Pretendard`, `Apple SD Gothic Neo`, `Noto Sans KR`, `IBM Plex Sans`, `ui-sans-serif`, `system-ui`, `-apple-system`, `Segoe UI`, `Roboto`, `sans-serif`
- Mono: system tabular numerals via `font-variant-numeric: tabular-nums`
- Serif: none

### Rules

- Body text must stay at or above `13px`; dense metadata may use `12px`.
- Use `word-break: keep-all` and strict line breaking for Korean-friendly wrapping.
- Dates and counters use tabular numerals.

## 4. Spacing & Layout

### Base Unit

All spacing derives from a base of **4px**.

| Token | Value | Usage |
|-------|-------|-------|
| `--space-1` | `4px` | Tight inline separation |
| `--space-2` | `8px` | Chips, compact row gaps |
| `--space-3` | `12px` | Filter row gaps |
| `--space-4` | `16px` | Mobile page padding, grid gap |
| `--space-5` | `20px` | Modal mobile padding |
| `--space-6` | `24px` | Page padding, filter padding |
| `--space-8` | `32px` | Modal desktop padding |
| `--space-10` | `40px` | Footer separation |
| `--space-12` | `48px` | Empty state vertical padding |

### Grid

- Max content width: `1200px`.
- Event grid: responsive auto-fill cards with `minmax(330px, 1fr)`, collapsing to one column below `720px`.
- Breakpoint in current UI: `720px`.

### Rules

- Keep page sections full-width with constrained inner content.
- Cards use compact spacing because this is an operational browsing tool.
- Fixed-format controls should not resize on hover or loading state.

## 5. Components

### Site Header

- **Structure**: title only.
- **Variants**: light/dark only.
- **Spacing**: `28px 24px 22px` desktop, `16px` horizontal on mobile.
- **States**: none beyond text rendering.
- **Accessibility**: concise text heading.
- **Motion**: none.

### Filter Bar

- **Structure**: list select (`국내 행사 일정`, `글로벌 주요 행사`), event-scope select (`주요 분야`, `모든 행사 보기`), category select, venue select, search input, live count.
- **Variants**: sticky desktop/mobile wrapping layout.
- **Spacing**: `12px` row gap, `24px` desktop horizontal padding, `16px` mobile.
- **States**: focus uses accent outline; load count updates through `aria-live`.
- **Accessibility**: all controls have labels.
- **Motion**: none.

### Event Card

- **Structure**: button card with date, event name, venue, chips.
- **Variants**: category/status/cost chip combinations.
- **Spacing**: `16px 18px 14px`, `9px` internal gap.
- **States**: default, hover, focus-visible.
- **Accessibility**: card is a real button with detail-focused aria label.
- **Motion**: border and shadow transitions at `150ms`.

### Modal

- **Structure**: fixed overlay, detail panel, close button, title, category chips, visitor-facing event facts, cost/deadline facts, action-signal chips, action links, and a concise official-page reminder.
- **Variants**: loading, success, error.
- **Spacing**: `28px 32px 24px` desktop, `22px 20px` mobile.
- **States**: overlay click, Escape, and close button all dismiss.
- **Accessibility**: dialog uses `role="dialog"`, `aria-modal`, `aria-labelledby`, focuses the close button after final content renders, and restores focus on close.
- **Motion**: none.

### Footer Links

- **Structure**: one factual service sentence and compact secondary links.
- **Variants**: only essential integration/help links belong here; avoid top-level API navigation.
- **States**: links use dotted underline and accent hover.
- **Accessibility**: nav has an explicit `aria-label`.
- **Motion**: none.

## 6. Motion & Interaction

### Timing

| Type | Duration | Easing | Usage |
|------|----------|--------|-------|
| Micro | `150ms` | ease | Link/card/control hover |
| Skeleton | `1300ms` | linear transform | Placeholder shimmer |

### Rules

- Use `transform`, opacity, color, border-color, and shadow transitions only.
- Every clickable element must have keyboard focus styling.
- Escape and pointer dismissal are required for modal overlays.

## 7. Depth & Surface

### Strategy

Mixed, but restrained: borders define normal hierarchy; shallow shadows appear only on hover cards and modal elevation.

| Level | Value | Usage |
|-------|-------|-------|
| Border/default | `1px solid var(--rule)` | Header, filters, cards, footer, modal |
| Hover/shallow | `0 1px 0 var(--accent), 0 4px 16px rgba(13, 85, 99, 0.08)` | Focused/hovered event card |
| Modal/prominent | `0 8px 40px rgba(0,0,0,0.18)` | Dialog surface |

### Rules

- Do not add decorative shadows to page sections.
- Modal overlay is the only full-screen translucent layer.
- Repeated item cards may have borders; page sections should not become cards.
