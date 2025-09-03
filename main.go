package caddy_thumbs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/chai2010/webp"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/certmagic"
	"github.com/nfnt/resize"
	"go.uber.org/zap"
)

const (
	SCALE_MODE_M           = 0
	SCALE_MODE_W           = 1
	SCALE_MODE_WLT         = 2
	SCALE_MODE_WLC         = 3
	SCALE_MODE_WLB         = 4
	SCALE_MODE_WRT         = 5
	SCALE_MODE_WRC         = 6
	SCALE_MODE_WRB         = 7
	SCALE_MODE_WCC         = 8
	SCALE_MODE_WCT         = 9
	SCALE_MODE_WCB         = 10
	CROP_MODE_LEFTTOP      = 11
	CROP_MODE_LEFTMIDDLE   = 12
	CROP_MODE_LEFTBOTTOM   = 13
	CROP_MODE_RIGHTTOP     = 14
	CROP_MODE_RIGHTMIDDLE  = 15
	CROP_MODE_RIGHTBOTTOM  = 16
	CROP_MODE_CENTERTOP    = 17
	CROP_MODE_CENTERCENTER = 18
	CROP_MODE_CENTERBOTTOM = 19
)

var cropModeMap = map[string]int{
	"m":   SCALE_MODE_M,
	"w":   SCALE_MODE_W,
	"wlt": SCALE_MODE_WLT,
	"wlc": SCALE_MODE_WLC,
	"wlb": SCALE_MODE_WLB,
	"wrt": SCALE_MODE_WRT,
	"wrc": SCALE_MODE_WRC,
	"wrb": SCALE_MODE_WRB,
	"wct": SCALE_MODE_WCT,
	"wcc": SCALE_MODE_WCC,
	"wcb": SCALE_MODE_WCB,
	"wc":  SCALE_MODE_WCC,
	"lt":  CROP_MODE_LEFTTOP,
	"lc":  CROP_MODE_LEFTMIDDLE,
	"lb":  CROP_MODE_LEFTBOTTOM,
	"rt":  CROP_MODE_RIGHTTOP,
	"rc":  CROP_MODE_RIGHTMIDDLE,
	"rb":  CROP_MODE_RIGHTBOTTOM,
	"ct":  CROP_MODE_CENTERTOP,
	"cc":  CROP_MODE_CENTERCENTER,
	"cb":  CROP_MODE_CENTERBOTTOM,
	"c":   CROP_MODE_CENTERCENTER,
}

func init() {
	caddy.RegisterModule(ThumbsServer{})
	httpcaddyfile.RegisterHandlerDirective("thumbs_server", parseCaddyfile)
}

// ThumbsServer 实现一个缩略图生成服务器
type ThumbsServer struct {
	ImageStorageRaw  json.RawMessage `json:"image_storage,omitempty" caddy:"namespace=caddy.storage inline_key=module"`
	ThumbsStorageRaw json.RawMessage `json:"thumbs_storage,omitempty" caddy:"namespace=caddy.storage inline_key=module"`

	imageStorage  certmagic.Storage
	thumbsStorage certmagic.Storage
	ctx           caddy.Context

	MaxDimension   int    `json:"max_dimension,omitempty"`
	DefaultQuality int    `json:"default_quality,omitempty"`
	CacheControl   string `json:"cache_control,omitempty"`
	logger         *zap.Logger
	regex          *regexp.Regexp // 实例特定的正则表达式
}

// CaddyModule 返回模块信息
func (ThumbsServer) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.thumbs_server",
		New: func() caddy.Module { return new(ThumbsServer) },
	}
}

// Provision 设置模块
func (t *ThumbsServer) Provision(ctx caddy.Context) error {
	t.logger = ctx.Logger(t)

	// 设置默认值
	if t.MaxDimension == 0 {
		t.MaxDimension = 2000
	}
	if t.DefaultQuality == 0 {
		t.DefaultQuality = 85
	}
	if t.CacheControl == "" {
		t.CacheControl = "public, max-age=31536000" // 默认缓存一年
	}

	if t.ImageStorageRaw != nil {
		storageMod, err := ctx.LoadModule(t, "ImageStorageRaw")
		if err != nil {
			return fmt.Errorf("loading image storage module: %v", err)
		}
		t.imageStorage, _ = storageMod.(caddy.StorageConverter).CertMagicStorage()
	} else {
		return fmt.Errorf("image_storage is required")
	}

	if t.ThumbsStorageRaw != nil {
		storageMod, err := ctx.LoadModule(t, "ThumbsStorageRaw")
		if err != nil {
			return fmt.Errorf("loading image storage module: %v", err)
		}
		t.thumbsStorage, _ = storageMod.(caddy.StorageConverter).CertMagicStorage()
	} else {
		return fmt.Errorf("thumbs_storage is required")
	}

	t.regex = regexp.MustCompile(`^.*\/(([a-z]+)(\d+)x(\d+)(?:,([a-fA-F0-9]{6}|[a-fA-F0-9]{8}))?(?:,q(\d+))?(?:,(\w+))?)\/((?:.+)(\.\w+))$`)
	t.ctx = ctx
	return nil
}

// Validate 验证配置
func (t *ThumbsServer) Validate() error {
	if t.MaxDimension <= 0 {
		return errors.New("max_dimension must be positive")
	}
	if t.DefaultQuality < 0 || t.DefaultQuality > 100 {
		return errors.New("default_quality must be between 0 and 100")
	}
	return nil
}

// ServeHTTP 处理HTTP请求
func (t ThumbsServer) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// 解析请求路径，提取模式、尺寸信息和原始图片路径
	path := r.URL.Path
	matches := t.regex.FindStringSubmatch(path)

	if len(matches) < 8 {
		return caddyhttp.Error(http.StatusNotFound, errors.New("invalid thumbnail request format"))
	}

	modeDir := matches[1]
	mode := matches[2] // 获取模式字符
	width, _ := strconv.Atoi(matches[3])
	height, _ := strconv.Atoi(matches[4])
	bgColorHex := matches[5]
	qualityStr := matches[6]
	imagePath := matches[8]
	format := matches[9]

	// 验证尺寸是否超过限制
	if err := t.validateDimensions(width, height); err != nil {
		t.logger.Warn("Dimension validation failed", zap.Error(err))
		return caddyhttp.Error(http.StatusBadRequest, err)
	}

	// 解析质量参数
	quality := t.DefaultQuality
	if qualityStr != "" {
		if q, err := strconv.Atoi(qualityStr); err == nil && q >= 0 && q <= 100 {
			quality = q
		}
	}

	// 解析背景颜色
	var bgColor color.Color = color.White
	if bgColorHex != "" {
		if c, err := parseHexColor(bgColorHex); err == nil {
			bgColor = c
		}
	}

	// 构建缩略图路径和原始图片路径
	thumbPath := filepath.Join("/", modeDir, imagePath)
	originalPath := filepath.Join("/", imagePath)

	// 检查缩略图是否已存在
	if t.thumbsStorage.Exists(t.ctx, thumbPath) {
		t.logger.Info("Serving existing thumbnail", zap.String("path", thumbPath))

		gobytes, err := t.thumbsStorage.Load(t.ctx, thumbPath)
		if err != nil {
			return caddyhttp.Error(http.StatusInternalServerError, err)
		}
		reader := bytes.NewReader(gobytes)

		// 设置缓存头,写出文件内容
		t.setCacheHeaders(w)
		http.ServeContent(w, r, filepath.Base(thumbPath), time.Now(), reader)
		return nil
	}

	t.logger.Info("Thumbnail not found, generating new one", zap.String("path", thumbPath))

	// 检查原始图片是否存在
	if !t.imageStorage.Exists(t.ctx, originalPath) {
		t.logger.Error("Original image not found", zap.String("path", originalPath))
		return caddyhttp.Error(http.StatusNotFound, fmt.Errorf("original image not found: %s", imagePath))
	}

	// 从存储中读取原始图片
	gobytes, err := t.imageStorage.Load(t.ctx, imagePath)
	if err != nil {
		return caddyhttp.Error(http.StatusInternalServerError, err)
	}
	reader := bytes.NewReader(gobytes)

	result, err := t.generateThumbnail(reader, uint(width), uint(height), mode, bgColor, quality, format)
	if err != nil {
		t.logger.Error("Failed to generate thumbnail", zap.Error(err))
		return fmt.Errorf("unsupported thumbnail mode: %s", mode)
	}

	t.logger.Info("Generated and served new thumbnail",
		zap.String("path", thumbPath),
		zap.String("mode", mode),
		zap.Int("quality", quality),
		zap.String("format", format))

	// 保存缩略图到存储
	err = t.thumbsStorage.Store(t.ctx, thumbPath, result)
	if err != nil {
		return caddyhttp.Error(http.StatusInternalServerError, err)
	}

	// 发送缩略图到客户端
	t.setCacheHeaders(w)
	http.ServeContent(w, r, filepath.Base(thumbPath), time.Now(), bytes.NewReader(result))
	return nil
}

// setCacheHeaders 设置缓存头
func (t ThumbsServer) setCacheHeaders(w http.ResponseWriter) {
	if t.CacheControl != "" {
		w.Header().Set("Cache-Control", t.CacheControl)
		w.Header().Set("Expires", time.Now().AddDate(1, 0, 0).Format(http.TimeFormat))
	}
}

// validateDimensions 验证尺寸是否超过限制
func (t ThumbsServer) validateDimensions(width, height int) error {
	if width > t.MaxDimension || height > t.MaxDimension {
		return fmt.Errorf("dimensions too large: %dx%d (max: %dx%d)", width, height, t.MaxDimension, t.MaxDimension)
	}

	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid dimensions: %dx%d", width, height)
	}

	return nil
}

func (t ThumbsServer) generateThumbnail(reader io.Reader, width, height uint, mode string, bgColor color.Color, quality int, format string) (buf []byte, err error) {
	// 解码图片
	var img image.Image
	img, err = t.decodeImage(reader)
	if err != nil {
		return nil, err
	}
	// 解析裁剪模式
	modeId, ok := cropModeMap[mode]
	if !ok {
		return nil, fmt.Errorf("unsupported thumbnail mode: %s", mode)
	}
	// 根据模式生成缩略图
	switch modeId {
	case SCALE_MODE_M:
		newImg := resize.Thumbnail(width, height, img, resize.Lanczos3)
		return t.encodeImage(newImg, quality, format)
	case SCALE_MODE_WLT, SCALE_MODE_WLC, SCALE_MODE_WLB, SCALE_MODE_WRT, SCALE_MODE_WRC, SCALE_MODE_WRB, SCALE_MODE_WCC, SCALE_MODE_WCT, SCALE_MODE_WCB:
		newImg := t.generateThumbnailModeW(img, width, height, bgColor, modeId)
		return t.encodeImage(newImg, quality, format)
	case CROP_MODE_LEFTTOP, CROP_MODE_LEFTMIDDLE, CROP_MODE_LEFTBOTTOM, CROP_MODE_RIGHTTOP, CROP_MODE_RIGHTMIDDLE, CROP_MODE_RIGHTBOTTOM, CROP_MODE_CENTERTOP, CROP_MODE_CENTERCENTER, CROP_MODE_CENTERBOTTOM:
		newImg := t.generateThumbnailModeCrop(img, width, height, modeId)
		return t.encodeImage(newImg, quality, format)
	}
	return nil, fmt.Errorf("unsupported thumbnail mode: %s", mode)
}

// generateThumbnailModeW 模式w：保持纵横比，缩放到目标尺寸以内，然后将不足的部分填充为指定颜色
func (t ThumbsServer) generateThumbnailModeW(img image.Image, width, height uint, bgColor color.Color, modeId int) image.Image {
	// 生成缩略图（保持纵横比）
	resized := resize.Thumbnail(width, height, img, resize.Lanczos3)

	// 创建目标大小的画布,根据颜色值填充背景色
	canvas := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)
	var (
		resizedBounds                   = resized.Bounds()
		resizedWidth, resizedHeight     = resizedBounds.Dx(), resizedBounds.Dy()
		x, y                        int = (int(width) - resizedWidth) / 2, (int(height) - resizedHeight) / 2
	)
	if resizedWidth == int(width) {
		x = 0
		switch modeId {
		case SCALE_MODE_WLT, SCALE_MODE_WCT, SCALE_MODE_WRT:
			y = 0
		case SCALE_MODE_WLC, SCALE_MODE_WCC, SCALE_MODE_WRC:
			y = (int(height) - resizedHeight) / 2
		case SCALE_MODE_WLB, SCALE_MODE_WRB, SCALE_MODE_WCB:
			y = (int(height) - resizedHeight)
		}
	}
	if resizedHeight == int(height) {
		y = 0
		switch modeId {
		case SCALE_MODE_WLT, SCALE_MODE_WRT, SCALE_MODE_WCT:
			x = 0
		case SCALE_MODE_WLC, SCALE_MODE_WCC, SCALE_MODE_WRC:
			x = (int(width) - resizedWidth) / 2
		case SCALE_MODE_WLB, SCALE_MODE_WRB, SCALE_MODE_WCB:
			x = (int(width) - resizedWidth)
		}
	}
	// 将缩略图绘制到画布上
	draw.Draw(canvas, image.Rect(x, y, x+resizedWidth, y+resizedHeight), resized, image.Point{0, 0}, draw.Over)
	return canvas
}

func (t ThumbsServer) generateThumbnailModeCrop(img image.Image, width, height uint, cropMode int) image.Image {
	// 原始尺寸
	origBounds := img.Bounds()
	origWidth := uint(origBounds.Dx())
	origHeight := uint(origBounds.Dy())

	// 计算缩放比例
	widthRatio := float64(width) / float64(origWidth)
	heightRatio := float64(height) / float64(origHeight)
	scale := widthRatio
	if heightRatio > widthRatio {
		scale = heightRatio
	}

	// 缩放图片
	scaledWidth := uint(float64(origWidth) * scale)
	scaledHeight := uint(float64(origHeight) * scale)
	resized := resize.Resize(scaledWidth, scaledHeight, img, resize.Lanczos3)
	// 计算裁剪位置
	var (
		resizedBounds               = resized.Bounds()
		resizedWidth, resizedHeight = resizedBounds.Dx(), resizedBounds.Dy()
		// 计算裁剪位置
		x = (resizedWidth - int(width)) / 2
		y = (resizedHeight - int(height)) / 2
	)
	if resizedWidth == int(width) {
		x = 0
		switch cropMode {
		case CROP_MODE_LEFTTOP, CROP_MODE_CENTERTOP, CROP_MODE_RIGHTTOP:
			y = 0
		case CROP_MODE_LEFTMIDDLE, CROP_MODE_CENTERCENTER, CROP_MODE_RIGHTMIDDLE:
			y = int(math.Abs(float64((int(height) - resizedHeight) / 2)))
		case CROP_MODE_LEFTBOTTOM, CROP_MODE_CENTERBOTTOM, CROP_MODE_RIGHTBOTTOM:
			y = int(math.Abs(float64((int(height) - resizedHeight))))
		}
	}
	if resizedHeight == int(height) {
		y = 0
		switch cropMode {
		case CROP_MODE_LEFTTOP, CROP_MODE_LEFTMIDDLE, CROP_MODE_LEFTBOTTOM:
			x = 0
		case CROP_MODE_RIGHTTOP, CROP_MODE_RIGHTMIDDLE, CROP_MODE_RIGHTBOTTOM:
			x = int(math.Abs(float64((int(width) - resizedWidth))))
		case CROP_MODE_CENTERTOP, CROP_MODE_CENTERCENTER, CROP_MODE_CENTERBOTTOM:
			x = int(math.Abs(float64((int(width) - resizedWidth) / 2)))
		}
	}

	// 创建目标大小的画布
	canvas := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	t.logger.Info("x,y,w,h", zap.Int("x", x), zap.Int("y", y), zap.Int("width", resizedWidth), zap.Int("height", resizedHeight))
	// 绘制裁剪后的图片
	draw.Draw(canvas, canvas.Bounds(), resized, image.Point{x, y}, draw.Over)
	return canvas
}

var (
	jpegHeader  = []byte{0xFF, 0xD8}
	pngHeader   = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	webpHeader  = []byte("RIFF")
	webpHeader2 = []byte("WEBP")
	avifHeader  = []byte("ftyp")
)

// decodeImage 解码图片
func (t ThumbsServer) decodeImage(reader io.Reader) (image.Image, error) {
	var (
		buf     = make([]byte, 16)
		numRead int
		err     error
	)
	numRead, err = reader.Read(buf)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read file header: %v", err)
	}

	multiReader := io.MultiReader(bytes.NewReader(buf[:numRead]), reader)

	if numRead >= 2 {
		switch {
		case bytes.HasPrefix(buf, jpegHeader):
			return jpeg.Decode(multiReader)
		case bytes.HasPrefix(buf, pngHeader):
			return png.Decode(multiReader)
		case bytes.HasPrefix(buf, webpHeader):
			return webp.Decode(reader)
		case bytes.HasPrefix(buf, webpHeader2):
			return webp.Decode(reader)
		default:
			return nil, fmt.Errorf("unsupported image format")
		}
	}
	return nil, fmt.Errorf("unsupported image format, file header: %x", buf[:numRead])
}

// encodeImage 编码并保存图片
func (t ThumbsServer) encodeImage(img image.Image, quality int, format string) ([]byte, error) {
	// 写出到 io.Writer 最后返回 []byte

	var (
		buf    []byte
		err    error
		writer io.Writer = bytes.NewBuffer(buf)
	)

	// 根据格式保存图片
	switch format {
	case ".jpg", ".jpeg":
		err = jpeg.Encode(writer, img, &jpeg.Options{Quality: quality})
	case ".png":
		err = png.Encode(writer, img)
	case ".webp":
		err = webp.Encode(writer, img, &webp.Options{Quality: float32(quality)})
	default:
		return nil, fmt.Errorf("unsupported output format: %s", format)
	}
	if err != nil {
		return nil, err
	}
	return writer.(*bytes.Buffer).Bytes(), nil
}

// parseHexColor 解析十六进制颜色代码
func parseHexColor(s string) (color.RGBA, error) {
	if len(s) != 6 && len(s) != 8 {
		return color.RGBA{}, fmt.Errorf("invalid color length: %s (must be 6 or 8)", s)
	}

	value, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{}, fmt.Errorf("invalid color format: %s", s)
	}

	if len(s) == 6 {
		return color.RGBA{
			R: uint8(value >> 16),
			G: uint8((value >> 8) & 0xFF),
			B: uint8(value & 0xFF),
			A: 255,
		}, nil
	}

	return color.RGBA{
		R: uint8(value >> 24),
		G: uint8((value >> 16) & 0xFF),
		B: uint8((value >> 8) & 0xFF),
		A: uint8(value & 0xFF),
	}, nil
}

// UnmarshalCaddyfile 解析Caddyfile配置
func (t *ThumbsServer) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "max_dimension":
				if !d.NextArg() {
					return d.ArgErr()
				}
				if val, err := strconv.Atoi(d.Val()); err == nil {
					t.MaxDimension = val
				} else {
					return d.Errf("invalid max_dimension value: %s", d.Val())
				}
			case "default_quality":
				if !d.NextArg() {
					return d.ArgErr()
				}
				if val, err := strconv.Atoi(d.Val()); err == nil {
					t.DefaultQuality = val
				} else {
					return d.Errf("invalid default_quality value: %s", d.Val())
				}
			case "cache_control":
				if !d.NextArg() {
					return d.ArgErr()
				}
				t.CacheControl = d.Val()
			case "thumbs_storage":
				if t.ThumbsStorageRaw != nil {
					return d.Err("ThumbsStorageRaw already set.")
				}
				if !d.NextArg() {
					return d.ArgErr()
				}
				modStem := d.Val()
				modID := "caddy.storage." + modStem
				unm, err := caddyfile.UnmarshalModule(d, modID)
				if err != nil {
					return err
				}
				storage, ok := unm.(caddy.StorageConverter)
				if !ok {
					return d.Errf("module %s is not a caddy.StorageConverter", modID)
				}
				t.ThumbsStorageRaw = caddyconfig.JSONModuleObject(storage, "module", storage.(caddy.Module).CaddyModule().ID.Name(), nil)

			case "image_storage":
				if !d.NextArg() {
					return d.ArgErr()
				}
				modStem := d.Val()
				modID := "caddy.storage." + modStem
				unm, err := caddyfile.UnmarshalModule(d, modID)
				if err != nil {
					return err
				}
				storage, ok := unm.(caddy.StorageConverter)
				if !ok {
					return d.Errf("module %s is not a caddy.StorageConverter", modID)
				}
				t.ImageStorageRaw = caddyconfig.JSONModuleObject(storage, "module", storage.(caddy.Module).CaddyModule().ID.Name(), nil)
			default:
				return d.Errf("unrecognized subdirective: %s", d.Val())
			}
		}
	}
	return nil
}

// parseCaddyfile 解析Caddyfile
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var t ThumbsServer
	err := t.UnmarshalCaddyfile(h.Dispenser)
	return t, err
}

// Interface guards
var (
	_ caddy.Provisioner           = (*ThumbsServer)(nil)
	_ caddy.Validator             = (*ThumbsServer)(nil)
	_ caddyhttp.MiddlewareHandler = (*ThumbsServer)(nil)
	_ caddyfile.Unmarshaler       = (*ThumbsServer)(nil)
)
