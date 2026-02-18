# Afficho — Style Guide

Reference for all visual and UI decisions across the Afficho platform.
Keep this document up to date as the design evolves.

---

## Brand Name

- **Product name:** Afficho (capital A)
- **Wordmark:** afficho (all lowercase, used in logos and headings)
- **Tagline:** DIGITAL SIGNAGE (uppercase, spaced)
- **Origin:** French *affiche* (poster / sign)

---

## Color Palette

### Primary

| Name            | Hex       | Usage                                      |
|-----------------|-----------|---------------------------------------------|
| Violet          | `#6C3AED` | Primary brand color, buttons, links, accents |
| Blue            | `#2563EB` | Secondary brand color, interactive elements  |

The two primaries combine into the **brand gradient** used on the icon,
buttons, and highlighted text:

```css
background: linear-gradient(135deg, #6C3AED, #2563EB);
```

### Dark (Backgrounds)

| Name            | Hex       | Usage                                       |
|-----------------|-----------|----------------------------------------------|
| Navy 900        | `#0F172A` | Page background (display page, dark UI)       |
| Navy 800        | `#1E1B4B` | Card / panel background, screen interiors     |

### Neutral (Text & UI)

| Name            | Hex       | Usage                                       |
|-----------------|-----------|----------------------------------------------|
| Slate 600       | `#475569` | Secondary text, captions, status messages     |
| Slate 400       | `#94A3B8` | Placeholder text, disabled states             |
| Slate 200       | `#E2E8F0` | Borders, dividers                             |
| White           | `#FFFFFF` | Primary text on dark backgrounds              |

### Semantic

| Name            | Hex       | Usage                          |
|-----------------|-----------|---------------------------------|
| Success         | `#22C55E` | Positive status, confirmations  |
| Warning         | `#EAB308` | Cautions, attention-needed      |
| Error           | `#EF4444` | Errors, destructive actions     |

### Opacity Scale (on dark backgrounds)

White overlays at fixed opacities create depth without introducing new colors:

| Opacity | Use case                          |
|---------|-----------------------------------|
| 0.50    | Prominent icons on dark surfaces  |
| 0.35    | Secondary elements, signal waves  |
| 0.25    | Tertiary UI, stands, dividers     |
| 0.20    | Subtle background shapes          |
| 0.15    | Bezel / glass effect              |

---

## Typography

### Font Stack

```css
font-family: system-ui, -apple-system, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;
```

No custom web fonts. The system font stack keeps the binary dependency-free
and renders natively on every platform.

### Type Scale

| Element         | Size       | Weight | Letter-spacing | Notes                          |
|-----------------|------------|--------|----------------|--------------------------------|
| Wordmark        | 2.4 rem    | 700    | -0.5px         | Gradient text fill             |
| Page heading    | 1.5 rem    | 700    | -0.25px        |                                |
| Section heading | 1.125 rem  | 600    | normal         |                                |
| Body            | 1 rem      | 400    | normal         | Base size, 16px equivalent     |
| Caption / label | 0.875 rem  | 500    | 0.025em        |                                |
| Tagline         | 1 rem      | 400    | 0.2em          | Uppercase                      |
| Status message  | 1 rem      | 400    | 0.05em         | Slate 600 color                |

### Text on Dark vs Light

| Background | Primary text | Secondary text |
|------------|-------------|----------------|
| Dark       | `#FFFFFF`   | `#94A3B8`      |
| Light      | `#1E1B4B`   | `#475569`      |

### Gradient Text

Used for the wordmark and prominent headings on dark backgrounds:

```css
background: linear-gradient(135deg, #6C3AED, #2563EB);
-webkit-background-clip: text;
-webkit-text-fill-color: transparent;
background-clip: text;
```

---

## Logo & Icon

### Files

| Asset         | Path                     | Dimensions | Format |
|---------------|--------------------------|------------|--------|
| Icon          | `web/static/icon.svg`    | 512 x 512  | SVG    |
| Logo          | `web/static/logo.svg`    | 720 x 200  | SVG    |

### Icon Anatomy

The icon is a rounded square (`rx="96"` at 512px) filled with the brand
gradient. It contains:

1. **Screen** — dark interior with content wireframe (title, text lines,
   image placeholder), representing the signage display
2. **Play indicator** — violet circle with white triangle, bottom-right
   of the screen
3. **Broadcast waves** — two arcs in the top-right corner, representing
   the live/connected nature of the platform
4. **Stand** — subtle base below the screen

### Logo Anatomy

The horizontal logo places a compact icon (168 x 168) to the left of:

- **Wordmark:** "afficho" in Navy 800 (`#1E1B4B`), bold, 72px
- **Tagline:** "DIGITAL SIGNAGE" in Violet (`#6C3AED`), regular, 16px,
  letter-spacing 3px, 80% opacity

### Usage Rules

- Minimum icon size: 32 x 32px (favicon) — below this the details are lost
- Minimum logo width: 200px
- Clear space around the icon: at least 25% of its width on all sides
- Do not rotate, skew, recolor, or add effects (shadows, outlines)
- On dark backgrounds: use the icon as-is (the gradient reads well)
- On light backgrounds: use the icon as-is; use Navy 800 for the wordmark

### Favicon

Embedded as an inline SVG data URI in HTML pages:

```html
<link rel="icon" type="image/svg+xml" href="data:image/svg+xml,...">
```

No PNG fallback is needed — the target browser is Chromium, which supports
SVG favicons natively.

---

## Spacing & Layout

### Base Unit

`0.5rem` (8px). All spacing derives from multiples of this base.

| Token  | Value   | Use case                          |
|--------|---------|-----------------------------------|
| xs     | 0.25rem | Inline icon gaps                  |
| sm     | 0.5rem  | Tight padding (badges, chips)     |
| md     | 1rem    | Standard padding, element gaps    |
| lg     | 1.5rem  | Section padding                   |
| xl     | 2rem    | Major section gaps, splash layout |
| 2xl    | 3rem    | Page-level vertical rhythm        |

### Border Radius

| Token   | Value | Use case                        |
|---------|-------|---------------------------------|
| sm      | 4px   | Small elements (badges, chips)  |
| md      | 8px   | Cards, inputs, buttons          |
| lg      | 12px  | Panels, modals                  |
| xl      | 20px  | Prominent containers            |
| full    | 9999px| Pills, avatar circles           |
| icon    | 96px  | Icon at 512px (18.75% of width) |

---

## Components

### Buttons

**Primary** — brand gradient background, white text, `md` radius:

```css
background: linear-gradient(135deg, #6C3AED, #2563EB);
color: #FFFFFF;
border: none;
border-radius: 8px;
padding: 0.625rem 1.25rem;
font-weight: 600;
```

**Secondary** — transparent, violet border and text:

```css
background: transparent;
color: #6C3AED;
border: 1.5px solid #6C3AED;
border-radius: 8px;
padding: 0.625rem 1.25rem;
font-weight: 600;
```

**Destructive** — error red background, white text:

```css
background: #EF4444;
color: #FFFFFF;
border-radius: 8px;
```

### Cards (Admin UI)

```css
background: #1E1B4B;
border: 1px solid rgba(255, 255, 255, 0.1);
border-radius: 12px;
padding: 1.5rem;
```

### Inputs

```css
background: #0F172A;
color: #FFFFFF;
border: 1.5px solid #334155;
border-radius: 8px;
padding: 0.5rem 0.75rem;
```

Focus state: `border-color: #6C3AED; box-shadow: 0 0 0 3px rgba(108, 58, 237, 0.25);`

---

## Display Page

The fullscreen display page (`/display`) is the output surface rendered by
Chromium in kiosk mode. It has two visual states:

### Active Playback

- Background: `#0F172A` (hidden behind content)
- Content fills the entire viewport via `object-fit: contain` (images/video)
  or borderless iframes (url/html)
- No chrome, no padding, no overlays during playback

### Splash / Idle

Shown when no content is scheduled or the server is unreachable:

- Background: `#0F172A`
- Centered column layout with `2rem` gap
- Icon at `96 x 96px`, 90% opacity
- Wordmark in gradient text, `2.4rem` bold
- Status message in Slate 600, `1rem`

---

## CSS Variables Reference

For use when building new pages (admin UI, future overlays):

```css
:root {
  /* Brand */
  --color-violet:     #6C3AED;
  --color-blue:       #2563EB;
  --gradient-brand:   linear-gradient(135deg, #6C3AED, #2563EB);

  /* Backgrounds */
  --color-navy-900:   #0F172A;
  --color-navy-800:   #1E1B4B;

  /* Text */
  --color-slate-600:  #475569;
  --color-slate-400:  #94A3B8;
  --color-slate-200:  #E2E8F0;
  --color-white:      #FFFFFF;

  /* Semantic */
  --color-success:    #22C55E;
  --color-warning:    #EAB308;
  --color-error:      #EF4444;

  /* Typography */
  --font-sans: system-ui, -apple-system, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;

  /* Spacing */
  --space-xs:  0.25rem;
  --space-sm:  0.5rem;
  --space-md:  1rem;
  --space-lg:  1.5rem;
  --space-xl:  2rem;
  --space-2xl: 3rem;

  /* Radii */
  --radius-sm:   4px;
  --radius-md:   8px;
  --radius-lg:   12px;
  --radius-xl:   20px;
  --radius-full: 9999px;
}
```

---

## Dos and Don'ts

**Do:**
- Use the brand gradient for primary actions and emphasis
- Keep backgrounds dark (Navy 900/800) for the display-facing UI
- Rely on the system font stack everywhere
- Use white with opacity for layering depth on dark surfaces
- Maintain generous spacing (8px base grid)

**Don't:**
- Mix the brand violet/blue as flat colors side by side (use the gradient)
- Use pure black (`#000000`) as a background — use Navy 900 instead
- Add drop shadows on dark backgrounds (use borders or opacity layers)
- Use more than two font weights on a single screen
- Place low-opacity white text on light backgrounds
