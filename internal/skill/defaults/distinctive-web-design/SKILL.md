---
name: distinctive-web-design
description: Planning a website's visual design so it has a point of view instead of looking generic or obviously AI-generated. Use before building any landing page, marketing site, portfolio, blog or product UI — decide art direction, type, colour and layout first, then write CSS.
---

# Distinctive Web Design

Most generated sites converge on the same look: a centred 1200px column, a
purple-to-blue gradient hero, Inter everywhere, three equal feature cards with
line icons, `fade-in-on-scroll` on every section. It reads as "template". The
goal here is a site that looks like **someone decided how it should feel** — and
then defended that decision through every screen.

Do this planning **before** touching CSS. Design is decisions, not effects.

## 1. Pick a point of view first

Write one sentence: *"This site should feel like ___."* Concrete adjectives,
not "modern and clean" (everything claims that). Examples: "a printed technical
manual", "a gallery wall", "an editorial magazine spread", "a terminal",
"handmade zine", "a luxury watch catalogue".

Then pick **one memorable move** the site commits to — the thing a person would
describe afterwards. A giant serif headline. A visible baseline grid. Monochrome
until you hover. Oversized numbers. A hard-edged brutalist border. Editorial
pull-quotes. It only needs one, done confidently, on every page.

Gather 3–5 real references (sites, print, packaging — not "AI landing page
inspiration"). Name what you're stealing from each: *this typeface pairing*,
*that use of whitespace*, *this colour restraint*.

## 2. Kill the generic tells

These are the fingerprints of a thrown-together site. Avoid all of them:

- Default fonts: Inter, Roboto, Open Sans, system-ui as the *brand* face.
- The diagonal purple/indigo gradient. Glassmorphism cards. Neon glow on dark.
- A hero that is centred headline + subtitle + two pill buttons + dashboard
  screenshot, and nothing else.
- Three or four feature cards of equal size with a thin line icon on top.
- Every section the same full-width, same vertical rhythm, same centre alignment.
- Emoji as section icons. Gradient text. `box-shadow` on everything.
- Stock "diverse team pointing at a laptop" photography.
- Animating every element in on scroll.

## 3. Typography

- Choose a **display face with personality** for headings (a real serif, a grotesk
  with character, a mono) and a quiet, readable face for body. One pairing, used
  consistently. Variable fonts are fine; character matters more than novelty.
- Set a type scale with real contrast — headings that are genuinely large
  (clamp up to 3–6rem on a hero), body at a comfortable 16–20px, generous line
  height (1.5–1.7) and a measure of 60–75 characters.
- Details that signal care: tighten tracking on big headings, use real small
  caps or tabular figures where they apply, hang punctuation, curly quotes.

## 4. Colour

- Do not use the framework's default palette straight. Build from a source: a
  photo, a material, a brand ink, a single hue you rotate in lightness and
  saturation. 2–3 core colours plus neutrals is plenty.
- Commit to a **background that isn't `#fff` or `#0a0a0a`** — a warm paper, a
  cool off-black, a deep ink, a muted colour. It sets the whole mood in one line.
- One accent, used sparingly and always for the same meaning. Check contrast
  (WCAG AA: 4.5:1 body, 3:1 large text) in both themes.

## 5. Layout

- Break the reflex of one centred container. Use an asymmetric grid, a real
  12-column system with intentional spans, a wide left margin, content that
  overlaps or bleeds to the edge.
- Vary section rhythm: a tall hero, then a tight dense block, then air. Not every
  section the same height and padding.
- Align to a visible or implied grid and keep spacing on a scale (4 / 8 px steps
  or a modular scale). Consistency in the system is what lets you be bold with
  the exceptions.

## 6. Motion & detail (the last 10%)

- Motion has a job: reveal hierarchy, give feedback, ease a transition. One
  considered entrance beats fifty scroll-triggered fades. Keep it fast
  (150–300ms), respect `prefers-reduced-motion`.
- The craft signals: real `:focus-visible` styles, considered hover states,
  designed empty/loading/error states, custom list markers, a 404 with the same
  voice, selection colour, favicon and OG image that match the identity.

## Process

1. One-sentence feel + the one memorable move + references.
2. Type pairing and scale; colour palette from a source; background colour.
3. Design tokens (spacing scale, colour vars, type steps, radii, shadows) — one
   place, both themes.
4. Lay out the hero and one content section to prove the direction, then apply.
5. Pass for the last-10% details above.

## When reviewing a design, ask

- Could I describe this site in one sentence that isn't "modern and clean"?
- What is the one move it commits to? Is it on every page?
- Would this be mistaken for a default template or a generated landing page?
- Is the brand face something other than Inter/Roboto/system-ui?
- Is the background colour a decision, or just white?
- Does every section look the same, or is there rhythm and contrast?
- Do focus states, empty states and the 404 share the same voice?
