# Homebox Location Label Service

Small Homebox external label service for Brother/P-touch-style location labels.

It renders a PNG label with:

- a QR code or DataMatrix code on the left
- one visible text block on the right
- no visible asset ID
- default visible text selection aimed at Homebox location labels

The code still encodes the Homebox `URL` parameter so scanning the label opens the Homebox page. The visible text is intentionally location-only.

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

## Environment variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port. |
| `LABEL_CODE_TYPE` | `datamatrix` | `datamatrix`, `qr`, or `none`. |
| `LABEL_TEXT_SOURCE` | `location` | Which query value becomes visible text. |
| `LABEL_DEFAULT_WIDTH` | `696` | Fallback label width in px. |
| `LABEL_DEFAULT_HEIGHT` | `128` | Fallback label height in px. |
| `LABEL_DEFAULT_DPI` | `180` | Fallback DPI. Brother P-touch is often 180 dpi. |
| `LABEL_MARGIN` | `8` | Outer margin in px. |
| `LABEL_COMPONENT_PADDING` | `10` | Gap between code and text in px. |
| `LABEL_CODE_SIZE` | `0` | `0` means auto-size from label height. |
| `LABEL_FONT_SIZE` | `0` | `0` means auto-size from label height or Homebox font size. |
| `LABEL_MAX_TEXT_LINES` | `2` | Max visible text lines. |
| `LABEL_LOG_REQUESTS` | `false` | Log incoming render requests. |
| `LABEL_TRIM_URL_FOR_CODE` | `false` | If true, trims `LABEL_HOMEBOX_BASE_URL` from the encoded code data. |
| `LABEL_HOMEBOX_BASE_URL` | empty | Base URL to trim from QR/DataMatrix content when trimming is enabled. |

## Build and publish

This repository uses the same GHCR build workflow style as `AutoConnectDockerNetworkToTraefik`:

- push to `master` publishes `latest`
- tags like `v1.0.0` publish semver tags
- manual `workflow_dispatch` is supported
- image name is derived from `${GITHUB_REPOSITORY,,}` so GHCR receives a lowercase path

Example published image for this repo name:

```text
ghcr.io/lukasl28/homebox-location-label-service:latest
```

## Deploy with your existing Homebox setup

Homebox should call the label service over Docker-internal DNS. It does not need to be public through Traefik.

Add the label service to the same Docker network as Homebox and add this environment variable to Homebox:

```yaml
services:
  homebox-label-service:
    image: ghcr.io/lukasl28/homebox-location-label-service:latest
    container_name: homebox-label-service
    restart: unless-stopped
    environment:
      - PORT=8080
      - LABEL_CODE_TYPE=datamatrix
      - LABEL_TEXT_SOURCE=location
    networks:
      - homebox_proxy
    read_only: true
    tmpfs:
      - /tmp
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL

  homebox:
    environment:
      - HBOX_LABEL_MAKER_LABEL_SERVICE_URL=http://homebox-label-service:8080/
    networks:
      - homebox_proxy

networks:
  homebox_proxy:
    external: true
    name: traefik_home_box
```

Keep all your existing Homebox volumes, Traefik labels and other environment variables. Only add the label-service URL and make sure both containers share one Docker network.

## Optional Traefik route for manual testing

Not required for Homebox, but useful if you want to open generated labels in a browser:

```yaml
labels:
  - traefik.enable=true
  - traefik.docker.network=traefik_home_box
  - traefik.http.routers.homebox-label-service.rule=Host(`label.familielandes.de`)
  - traefik.http.services.homebox-label-service.loadbalancer.server.port=8080
  - autoconnectdockernetworktotraefik.enable=true
```

## Test URL

From any container on the same Docker network:

```bash
curl -o label.png "http://homebox-label-service:8080/?Width=696&Height=128&Dpi=180&QrSize=96&URL=https%3A%2F%2Flager.familielandes.de%2Flocations%2Fexample&TitleText=Regal%20Rot"
```

The PNG should show a code on the left and `Regal Rot` as the only visible text.


## GitHub Actions build note

The `go.sum` file must be committed. The Docker build copies `go.mod` and `go.sum` before downloading modules, so GHCR builds are reproducible and do not fail with missing checksum entries.

