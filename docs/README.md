# WarpDL Documentation

This is the documentation site for [WarpDL](https://github.com/warpdl/warpdl), a powerful and versatile cross-platform download manager.

Built with [Astro](https://astro.build) and [Starlight](https://starlight.astro.build).

## Development

All commands are run from the `docs/` directory:

| Command         | Action                                       |
| :-------------- | :------------------------------------------- |
| `bun install`   | Install dependencies                         |
| `bun dev`       | Start local dev server at `localhost:4321`   |
| `bun build`     | Build production site to `./dist/`           |
| `bun preview`   | Preview build locally before deploying       |

## Project Structure

```
.
├── public/           # Static assets (favicons, etc.)
├── src/
│   ├── assets/       # Images embedded in docs
│   ├── content/
│   │   └── docs/     # Documentation pages (.md/.mdx)
│   └── content.config.ts
├── astro.config.mjs
└── package.json
```

Documentation pages live in `src/content/docs/`. Each file becomes a route based on its filename.

## Related

- [WarpDL Repository](https://github.com/warpdl/warpdl)
- [Report Bug](https://github.com/warpdl/warpdl/issues)
- [Request Feature](https://github.com/warpdl/warpdl/issues)
