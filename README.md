# Homebox Location Label Service

Small Homebox external label service for Brother/P-touch-style location labels.

It renders a PNG label with:

- a QR code or DataMatrix code on the left
- one visible text block on the right
- no visible Homebox asset ID
- no left/right outer margin by default
- automatic label width by default, so the PNG is only as wide as the code + gap + text

The code encodes the Homebox `URL` parameter so scanning the label opens the Homebox page. The visible text is intentionally location-only.

## Why Go?

The service is written in Go because it is a good fit for a small stateless HTTP image-rendering microservice. The runtime image is `scratch` and contains only the static binary.

## Homebox request compatibility

Homebox calls the external label service with a `GET` request and expects an image response. This service accepts the Homebox labelmaker query parameters:

- `Width`
- `Height`
- `Dpi`
- `Margin`
- `ComponentPadding`
- `QrSize`
- `URL`
- `TitleText`
- `TitleFontSize`
- `DescriptionText`
- `DescriptionFontSize`
- `AdditionalInformation`
- `DynamicLength`

Additional supported parameters:

- `AutoWidth`
- `DynamicWidth`
- `Gap`

Unknown parameters are ignored.

## Visible text behavior

Default:

```text
LABEL_TEXT_SOURCE=location
```

The service chooses the first useful value from:

1. `LocationName`
2. `LocationText`
3. `Location`
4. `ParentLocation`
5. `TitleText`, unless it looks like an asset ID such as `000-010`
6. `DescriptionText` as last fallback

For normal Homebox location labels, `TitleText` is the location name, so the label only prints the location name.

If you want to force a specific field:

```yaml
environment:
  - LABEL_TEXT_SOURCE=title
```

or:

```yaml
environment:
  - LABEL_TEXT_SOURCE=description
```

or use any raw query key:

```yaml
environment:
  - LABEL_TEXT_SOURCE=LocationName
```

## Auto width and margins

By default:

```text
LABEL_AUTO_WIDTH=true
LABEL_MARGIN=0
LABEL_GAP=8
```

This means the output width is calculated as:

```text
matrix width + gap + rendered text width
```

There is no extra white space on the left or right side. `LABEL_GAP` only controls the space between the matrix code and the text.

Set this if you want fixed-width labels again:

```yaml
environment:
  - LABEL_AUTO_WIDTH=false
  - LABEL_DEFAULT_WIDTH=696
```

## Matrix / QR URL prefix

Use `LABEL_URL_PREFIX` to prepend a value to the URL encoded into the QR/DataMatrix code.

Example:

```yaml
environment:
  - LABEL_URL_PREFIX=https://scanner.familielandes.de/?url=
```

If Homebox sends:

```text
https://lager.familielandes.de/locations/example
```

then the code contains:

```text
https://scanner.familielandes.de/?url=https://lager.familielandes.de/locations/example
```

If `LABEL_URL_PREFIX` is empty, the original Homebox URL is encoded unchanged.

## Environment variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port. |
| `LABEL_CODE_TYPE` | `datamatrix` | `datamatrix`, `qr`, or `none`. |
| `LABEL_TEXT_SOURCE` | `location` | Which query value becomes visible text. |
| `LABEL_DEFAULT_WIDTH` | `696` | Fallback fixed label width in px when auto width is disabled. |
| `LABEL_DEFAULT_HEIGHT` | `128` | Fallback label height in px. |
| `LABEL_DEFAULT_DPI` | `180` | Fallback DPI. Brother P-touch is often 180 dpi. |
| `LABEL_AUTO_WIDTH` | `true` | Shrink output width to the actual content width. |
| `LABEL_MAX_WIDTH` | `4096` | Safety cap for auto-width output. |
| `LABEL_MARGIN` | `0` | Vertical margin in px. Horizontal outer margin is always zero in the renderer. |
| `LABEL_GAP` | `8` | Gap between code and text in px. |
| `LABEL_COMPONENT_PADDING` | `8` | Backward-compatible alias for the gap if `LABEL_GAP` is not set. |
| `LABEL_CODE_SIZE` | `0` | `0` means auto-size from label height. |
| `LABEL_FONT_SIZE` | `0` | `0` means auto-size from label height or Homebox font size. |
| `LABEL_MAX_TEXT_LINES` | `1` | Max visible text lines. |
| `LABEL_URL_PREFIX` | empty | Prefix prepended to the Homebox URL before it is encoded into the code. |
| `LABEL_LOG_REQUESTS` | `false` | Log incoming render requests. |
| `LABEL_TRIM_URL_FOR_CODE` | `false` | If true, trims `LABEL_HOMEBOX_BASE_URL` from the encoded code data before adding `LABEL_URL_PREFIX`. |
| `LABEL_HOMEBOX_BASE_URL` | empty | Base URL to trim from QR/DataMatrix content when trimming is enabled. |

## Build and publish

This repository uses the same GHCR build workflow style as `AutoConnectDockerNetworkToTraefik`:

- push to `master` publishes `latest`
- tags like `v1.0.0` publish semver tags
- manual `workflow_dispatch` is supported
- image name is derived from `${GITHUB_REPOSITORY,,}` so GHCR receives a lowercase path

Example published image for your current repo name `LukasL28/HomeBox_LabelService`:

```text
ghcr.io/lukasl28/homebox_labelservice:latest
```

## Deploy with your existing Homebox setup

Homebox should call the label service over Docker-internal DNS. It does not need to be public through Traefik.

Add the label service to the same internal Docker network as Homebox and add this environment variable to Homebox:

```yaml
services:
  homebox-label-service:
    image: ghcr.io/lukasl28/homebox_labelservice:latest
    container_name: homebox-label-service
    restart: unless-stopped
    environment:
      - PORT=8080
      - LABEL_CODE_TYPE=datamatrix
      - LABEL_TEXT_SOURCE=location
      - LABEL_DEFAULT_HEIGHT=128
      - LABEL_DEFAULT_DPI=180
      - LABEL_AUTO_WIDTH=true
      - LABEL_GAP=8
      - LABEL_URL_PREFIX=
    networks:
      - internal
    read_only: true
    tmpfs:
      - /tmp:rw,noexec,nosuid,size=16m
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    healthcheck:
      test: ["CMD", "/homebox-location-label-service", "-healthcheck"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s

  homebox:
    environment:
      - HBOX_LABEL_MAKER_LABEL_SERVICE_URL=http://homebox-label-service:8080/
    networks:
      - internal

networks:
  internal:
    internal: true
```

Keep all your existing Homebox volumes, Traefik labels and other environment variables. Only add the label-service URL and make sure both containers share one Docker network.

## Test URL

From any container on the same Docker network:

```bash
curl -o label.png "http://homebox-label-service:8080/?Width=696&Height=128&Dpi=180&QrSize=96&URL=https%3A%2F%2Flager.familielandes.de%2Flocations%2Fexample&TitleText=Regal%20Rot"
```

The PNG should show a code on the left and `Regal Rot` as the only visible text. Its width should shrink to the actual content instead of staying at `696` px.

## GitHub Actions build note

The `go.sum` file must be committed. The Docker build copies `go.mod` and `go.sum` before downloading modules, so GHCR builds are reproducible and do not fail with missing checksum entries.
