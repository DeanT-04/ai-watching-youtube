# GitHub Docs — README reference library

Official GitHub Docs pages about README files and GitHub Flavored Markdown, fetched from
[github/docs](https://github.com/github/docs) (`main` branch) and stripped of YAML frontmatter.
Use these instead of fetching the web — they are the source of truth for *how GitHub renders*
Markdown. For *what makes a good README* (structure, length, tone), follow SKILL.md, not these
pages: the docs describe syntax, not taste.

## Index

| File | What it covers |
|---|---|
| `about-readmes.md` | What a README is for, where GitHub looks for it (`.github/` → root → `docs/`), 500 KiB truncation, profile READMEs |
| `quickstart-for-writing-on-github.md` | Fast intro to GitHub Flavored Markdown |
| `about-writing-and-formatting-on-github.md` | Overview of writing/formatting on GitHub |
| `basic-writing-and-formatting-syntax.md` | **The big one**: headings, text styling, quotes, code, color models, links, section links, relative links, custom anchors, line breaks, images, lists, task lists, mentions, emoji, footnotes, alerts, comments, escaping |
| `organizing-information-with-tables.md` | Tables: syntax, alignment, pipes, formatting inside cells |
| `organizing-information-with-collapsed-sections.md` | `<details>` / `<summary>` collapsible sections |
| `creating-and-highlighting-code-blocks.md` | Fenced code blocks, syntax highlighting, mermaid/geojson/stl block types |
| `creating-diagrams.md` | Mermaid diagrams, checking the Mermaid version, GeoJSON/TopoJSON/STL |
| `writing-mathematical-expressions.md` | Math via `$...$` / `$$...$$` |
| `autolinked-references-and-urls.md` | Auto-linking URLs, issues/PRs, commit SHAs, mentions |
| `about-tasklists.md` | Task lists `- [ ]` / `- [x]` |
| `creating-a-permanent-link-to-a-code-snippet.md` | Permanent links to code lines in the repo |
| `getting-permanent-links-to-files.md` | Permanent links to files (commit/branch scoped) |
| `licensing-a-repository.md` | License files, what GitHub shows, choosing a license |
| `customizing-your-repositorys-social-media-preview.md` | Social preview image (repo banner shown on socials/OG) |
| `about-wikis.md` | When to use a wiki instead of the README (longer docs) |

## When to read which file

- Writing a README from scratch → `about-readmes.md` + `basic-writing-and-formatting-syntax.md` (skim SKILL.md's structure first).
- Adding a diagram (flow/architecture) → `creating-diagrams.md` (Mermaid; GitHub renders ` ```mermaid ` blocks natively, supports `%%{init}%%` theme directives).
- Badges → not covered by GitHub Docs (shields.io is third-party). Follow SKILL.md's badge rules (only true badges, theme them, verify they resolve).
- Tables vs. alternatives → `organizing-information-with-tables.md`; prefer a diagram or bullets when the table gets wide (SKILL.md).
- Banner/social image → `customizing-your-repositorys-social-media-preview.md`.
- License section → `licensing-a-repository.md`; always end the README with License.
