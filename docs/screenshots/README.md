# Screenshots

This directory holds PNG screenshots referenced from the
[user guide](../user-guide.md#4-architecture-browser) (§4 Architecture
browser).

To contribute screenshots, run the daemon locally on the archai repo
itself (`archai serve --http :8080`) and capture each view at roughly
1440×900. Commit PNGs with the filenames below.

| Filename              | View            | Route         |
|-----------------------|-----------------|---------------|
| `dashboard.png`       | Dashboard       | `/`           |
| `layers.png`          | Layers          | `/layers`     |
| `packages.png`        | Packages list   | `/packages`   |
| `package-detail.png`  | Package detail  | `/packages/{path}` |
| `type-detail.png`     | Type detail     | `/types/{pkg}.{type}` |
| `configs.png`         | Configs         | `/configs`    |
| `targets.png`         | Targets         | `/targets`    |
| `diff.png`            | Diff            | `/diff`       |
| `search.png`          | Search          | `/search`     |

Keep images under 300 KB each — compress with
[oxipng](https://github.com/shssoichiro/oxipng) or
[pngquant](https://pngquant.org/) before committing.
