# nwm recipe registry

The index of recipes for [nwm](https://github.com/DasDarki/nwm), the nanos world manager.
There is no package server: every entry points to a public GitHub repository, and every
version is a git tag (`v1.2.0`) in that repository.

## Using recipes

```sh
nwm recipe search roleplay
nwm server create rp --recipe @dasdarki/roleplay@^1
```

## Publishing

1. Put a `Recipefile` (or `recipe.toml`) with a scoped name like `@your-login/my-recipe` into a public GitHub repository
   (at the root or in a subdirectory).
2. Run `nwm recipe publish` inside it. This opens an issue here, using the `gh` CLI if available.
3. The bot validates the request, adds `recipes/<scope>/<name>.json`, rebuilds `index.json` and closes the issue.
4. Push version tags like `v0.1.0`. Users resolve `@^0.1` style constraints against them.

A scope is yours if it equals your GitHub login or names an organization you are a public member of.
Running `nwm recipe publish` again updates the description, keywords or repository.
`nwm recipe unpublish @scope/name` removes an entry.

## Layout

| Path | Content |
|---|---|
| `recipes/<scope>/<name>.json` | One entry per recipe, written by the bot |
| `index.json` | All entries, fetched by nwm and cached for a day |
| `tools/registry` | The validator run by `.github/workflows/process.yml` |
