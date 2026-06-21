package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/datamatrix"
	"github.com/boombuler/barcode/qr"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var version = "dev"

var assetIDPattern = regexp.MustCompile(`^\s*[A-Za-z]*\d{2,4}[-_. ]?\d{2,6}\s*$`)

type config struct {
	Port             string
	CodeType         string
	TextSource       string
	DefaultWidth     int
	DefaultHeight    int
	DefaultDPI       float64
	DefaultMargin    int
	DefaultPadding   int
	DefaultCodeSize  int
	FontSize         float64
	MaxTextLines     int
	Foreground       color.Color
	Background       color.Color
	LogRequests      bool
	TrimURLForQR     bool
	HomeboxBaseURL   string
	ReadHeaderLimit  int
	ShutdownTimeout  time.Duration
}

type labelParams struct {
	Width               int
	Height              int
	DPI                 float64
	Margin              int
	Padding             int
	CodeSize            int
	TitleText           string
	DescriptionText     string
	AdditionalInfo      string
	URL                 string
	DynamicLength       bool
	TitleFontSize       float64
	DescriptionFontSize float64
	Raw                 url.Values
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "check the local HTTP health endpoint")
	flag.Parse()

	cfg := loadConfig()
	if *healthcheck {
		if err := runHealthcheck(cfg.Port); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleLabel(cfg))
	mux.HandleFunc("/label", handleLabel(cfg))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, "%s\n", version)
	})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		MaxHeaderBytes:    cfg.ReadHeaderLimit,
	}

	log.Printf("homebox-location-label-service listening on :%s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
}

func loadConfig() config {
	return config{
		Port:            envString("PORT", "8080"),
		CodeType:        strings.ToLower(envString("LABEL_CODE_TYPE", "datamatrix")),
		TextSource:      strings.ToLower(envString("LABEL_TEXT_SOURCE", "location")),
		DefaultWidth:    envInt("LABEL_DEFAULT_WIDTH", 696),
		DefaultHeight:   envInt("LABEL_DEFAULT_HEIGHT", 128),
		DefaultDPI:      envFloat("LABEL_DEFAULT_DPI", 180),
		DefaultMargin:   envInt("LABEL_MARGIN", 8),
		DefaultPadding:  envInt("LABEL_COMPONENT_PADDING", 10),
		DefaultCodeSize: envInt("LABEL_CODE_SIZE", 0),
		FontSize:        envFloat("LABEL_FONT_SIZE", 0),
		MaxTextLines:    envInt("LABEL_MAX_TEXT_LINES", 2),
		Foreground:      color.Black,
		Background:      color.White,
		LogRequests:     envBool("LABEL_LOG_REQUESTS", false),
		TrimURLForQR:    envBool("LABEL_TRIM_URL_FOR_CODE", false),
		HomeboxBaseURL:  strings.TrimRight(envString("LABEL_HOMEBOX_BASE_URL", ""), "/"),
		ReadHeaderLimit: envInt("LABEL_MAX_HEADER_BYTES", 16<<10),
		ShutdownTimeout: 5 * time.Second,
	}
}

func handleLabel(cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/label" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		params := parseParams(r.URL.Query(), cfg)
		labelText := selectVisibleText(params, cfg)
		if strings.TrimSpace(labelText) == "" {
			labelText = "Location"
		}

		if cfg.LogRequests {
			log.Printf("render label width=%d height=%d code=%s text=%q", params.Width, params.Height, cfg.CodeType, labelText)
		}

		img, err := renderLabel(params, cfg, labelText)
		if err != nil {
			log.Printf("render failed: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		if err := png.Encode(w, img); err != nil {
			log.Printf("png encode failed: %v", err)
		}
	}
}

func parseParams(q url.Values, cfg config) labelParams {
	width := clamp(parseInt(q.Get("Width"), cfg.DefaultWidth), 64, 4096)
	height := clamp(parseInt(q.Get("Height"), cfg.DefaultHeight), 32, 2048)
	margin := clamp(parseInt(q.Get("Margin"), cfg.DefaultMargin), 0, min(width, height)/3)
	padding := clamp(parseInt(q.Get("ComponentPadding"), cfg.DefaultPadding), 0, width/4)
	codeSize := parseInt(q.Get("QrSize"), cfg.DefaultCodeSize)
	if codeSize <= 0 {
		codeSize = min(height-(margin*2), height-4)
	}
	codeSize = clamp(codeSize, 16, min(width/2, max(16, height-(margin*2))))

	dpi := parseFloat(q.Get("Dpi"), cfg.DefaultDPI)
	if dpi <= 0 {
		dpi = cfg.DefaultDPI
	}

	return labelParams{
		Width:               width,
		Height:              height,
		DPI:                 dpi,
		Margin:              margin,
		Padding:             padding,
		CodeSize:            codeSize,
		TitleText:           strings.TrimSpace(q.Get("TitleText")),
		DescriptionText:     strings.TrimSpace(q.Get("DescriptionText")),
		AdditionalInfo:      strings.TrimSpace(firstNonEmpty(q.Get("AdditionalInformation"), q.Get("AdditionalInfo"), q.Get("ID"), q.Get("Id"))),
		URL:                 strings.TrimSpace(q.Get("URL")),
		DynamicLength:       parseBool(q.Get("DynamicLength"), false),
		TitleFontSize:       parseFloat(q.Get("TitleFontSize"), 0),
		DescriptionFontSize: parseFloat(q.Get("DescriptionFontSize"), 0),
		Raw:                 q,
	}
}

func selectVisibleText(p labelParams, cfg config) string {
	raw := p.Raw
	source := strings.ToLower(cfg.TextSource)

	switch source {
	case "title":
		return p.TitleText
	case "description":
		return p.DescriptionText
	case "additional", "additionalinformation", "id":
		return p.AdditionalInfo
	case "url":
		return p.URL
	case "location", "auto", "":
		// Homebox location labels usually arrive with the location name as TitleText.
		// Some future/custom integrations may send an explicit Location* parameter.
		text := firstNonEmpty(
			raw.Get("LocationName"),
			raw.Get("LocationText"),
			raw.Get("Location"),
			raw.Get("ParentLocation"),
		)
		if text != "" {
			return strings.TrimSpace(text)
		}
		if p.TitleText != "" && !looksLikeAssetID(p.TitleText) {
			return p.TitleText
		}
		// Last fallback keeps item/asset labels usable, but avoids printing the ID.
		return firstNonEmpty(p.DescriptionText, p.TitleText)
	default:
		// Allow direct use of arbitrary query parameter names.
		if v := raw.Get(cfg.TextSource); v != "" {
			return strings.TrimSpace(v)
		}
		return firstNonEmpty(p.DescriptionText, p.TitleText)
	}
}

func renderLabel(p labelParams, cfg config, labelText string) (*image.RGBA, error) {
	codeEnabled := cfg.CodeType != "none" && cfg.CodeType != "off" && cfg.CodeType != "false"
	codeSize := 0
	if codeEnabled {
		codeSize = p.CodeSize
	}

	imgW := p.Width
	imgH := p.Height
	if p.DynamicLength {
		// Keep Homebox-compatible behavior simple: never shrink, only allow more room for long visible text.
		imgW = max(imgW, estimateDynamicWidth(labelText, imgH))
	}

	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: cfg.Background}, image.Point{}, draw.Src)

	textX := p.Margin
	textWidth := imgW - (p.Margin * 2)
	if codeEnabled {
		codeData := p.URL
		if cfg.TrimURLForQR && cfg.HomeboxBaseURL != "" {
			codeData = strings.TrimPrefix(codeData, cfg.HomeboxBaseURL)
		}
		if codeData == "" {
			codeData = labelText
		}
		code, err := makeCode(codeData, cfg.CodeType, codeSize)
		if err != nil {
			return nil, err
		}
		codeY := (imgH - codeSize) / 2
		if codeY < p.Margin {
			codeY = p.Margin
		}
		draw.Draw(img, image.Rect(p.Margin, codeY, p.Margin+codeSize, codeY+codeSize), code, image.Point{}, draw.Over)
		textX = p.Margin + codeSize + p.Padding
		textWidth = imgW - textX - p.Margin
	}

	if textWidth < 12 {
		return img, nil
	}

	face, fontSize, lines := chooseTextLayout(labelText, p, cfg, textWidth, imgH-(p.Margin*2))

	metrics := face.Metrics()
	lineHeight := int(math.Ceil(float64(metrics.Height) / 64.0))
	totalTextHeight := lineHeight * len(lines)
	baseline := ((imgH - totalTextHeight) / 2) + int(math.Ceil(float64(metrics.Ascent)/64.0))
	if baseline < p.Margin+int(math.Ceil(float64(metrics.Ascent)/64.0)) {
		baseline = p.Margin + int(math.Ceil(float64(metrics.Ascent)/64.0))
	}

	drawer := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(cfg.Foreground),
		Face: face,
	}
	for i, line := range lines {
		line = ellipsize(line, face, textWidth)
		drawer.Dot = fixed.P(textX, baseline+(i*lineHeight))
		drawer.DrawString(line)
	}

	_ = fontSize
	return img, nil
}

func makeCode(data string, codeType string, size int) (image.Image, error) {
	if strings.TrimSpace(data) == "" {
		data = " "
	}
	var (
		code barcode.Barcode
		err  error
	)
	switch strings.ToLower(codeType) {
	case "qr", "qrcode":
		code, err = qr.Encode(data, qr.M, qr.Auto)
	case "datamatrix", "data-matrix", "matrix", "dm":
		code, err = datamatrix.Encode(data)
	default:
		return nil, fmt.Errorf("unsupported LABEL_CODE_TYPE %q; use datamatrix, qr, or none", codeType)
	}
	if err != nil {
		return nil, fmt.Errorf("create %s code: %w", codeType, err)
	}
	scaled, err := barcode.Scale(code, size, size)
	if err != nil {
		return nil, fmt.Errorf("scale code: %w", err)
	}
	return scaled, nil
}

func chooseTextLayout(text string, p labelParams, cfg config, maxWidth int, maxHeight int) (font.Face, float64, []string) {
	fontBytes := gobold.TTF
	fontObj, err := opentype.Parse(fontBytes)
	if err != nil {
		panic(err)
	}

	fontSize := cfg.FontSize
	if fontSize <= 0 {
		fontSize = p.TitleFontSize
	}
	if fontSize <= 0 {
		fontSize = math.Min(float64(p.Height)*0.42, 42)
	}
	if fontSize < 7 {
		fontSize = 7
	}

	maxLines := cfg.MaxTextLines
	if maxLines <= 0 {
		maxLines = 1
	}

	var lastFace font.Face
	var lastLines []string
	for size := fontSize; size >= 7; size -= 1 {
		face, err := opentype.NewFace(fontObj, &opentype.FaceOptions{
			Size:    size,
			DPI:     p.DPI,
			Hinting: font.HintingFull,
		})
		if err != nil {
			panic(err)
		}
		lines := wrapText(text, face, maxWidth, maxLines)
		metrics := face.Metrics()
		lineHeight := int(math.Ceil(float64(metrics.Height) / 64.0))
		fitsHeight := lineHeight*len(lines) <= maxHeight
		fitsWidth := true
		for _, line := range lines {
			if font.MeasureString(face, line).Ceil() > maxWidth {
				fitsWidth = false
				break
			}
		}

		lastFace = face
		lastLines = lines
		if fitsHeight && fitsWidth {
			return face, size, lines
		}
	}
	return lastFace, 7, lastLines
}

func wrapText(text string, face font.Face, maxWidth int, maxLines int) []string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return []string{""}
	}
	words := strings.Fields(text)
	var lines []string
	current := ""
	for _, word := range words {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if font.MeasureString(face, candidate).Ceil() <= maxWidth || current == "" {
			current = candidate
			continue
		}
		lines = append(lines, current)
		current = word
		if len(lines) >= maxLines {
			break
		}
	}
	if len(lines) < maxLines && current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		lines = []string{text}
	}
	return lines
}

func ellipsize(text string, face font.Face, maxWidth int) string {
	if font.MeasureString(face, text).Ceil() <= maxWidth {
		return text
	}
	runes := []rune(text)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + "…"
		if font.MeasureString(face, candidate).Ceil() <= maxWidth {
			return candidate
		}
	}
	return "…"
}

func estimateDynamicWidth(text string, height int) int {
	return max(256, len([]rune(text))*int(float64(height)*0.38)+height)
}

func runHealthcheck(port string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %s", resp.Status)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func looksLikeAssetID(s string) bool {
	return assetIDPattern.MatchString(s)
}

func parseInt(s string, fallback int) int {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return v
}

func parseFloat(s string, fallback float64) float64 {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return fallback
	}
	return v
}

func parseBool(s string, fallback bool) bool {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	v, err := strconv.ParseBool(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return v
}

func envString(name string, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func envInt(name string, fallback int) int {
	return parseInt(os.Getenv(name), fallback)
}

func envFloat(name string, fallback float64) float64 {
	return parseFloat(os.Getenv(name), fallback)
}

func envBool(name string, fallback bool) bool {
	return parseBool(os.Getenv(name), fallback)
}

func clamp(v, minValue, maxValue int) int {
	if maxValue < minValue {
		return minValue
	}
	if v < minValue {
		return minValue
	}
	if v > maxValue {
		return maxValue
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

