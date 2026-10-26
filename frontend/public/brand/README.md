# Outreach identity

- `mark.svg`: open O with an outward dot, ink `#0b0b0b`.
- `wordmark.svg`: the mark replaces the first o, followed by outlined Geist SemiBold lettering. No runtime font dependency.
- `app-icon.svg`: launcher master, including transparent platform margins.

The UI loads Geist locally from `../fonts`; its OFL license is included there.

From the repository root, run `pnpm --dir frontend exec playwright install chromium`, then `pnpm --dir frontend brand:build` to regenerate the macOS PNG and Windows ICO. Wails produces the macOS ICNS from the PNG during packaging. Commit the vector masters and generated icons together.
