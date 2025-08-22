package caddy_thumbs

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"net/http"
	"io"
	"os"
	"bytes"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/nfnt/resize"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(ThumbsServer{})
	httpcaddyfile.RegisterHandlerDirective("thumbs_server", parseCaddyfile)
}

// ThumbsServer 实现一个缩略图生成服务器
type ThumbsServer struct {
	ImageRoot     string `json:"image_root,omitempty"`
	ThumbsRoot    string `json:"thumbs_root,omitempty"`
	MaxDimension  int    `json:"max_dimension,omitempty"`
	DefaultQuality int   `json:"default_quality,omitempty"`
	CacheControl  string `json:"cache_control,omitempty"`
	logger        *zap.Logger
	regex         *regexp.Regexp // 实例特定的正则表达式
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

	if t.ThumbsRoot == "" {
		t.ThumbsRoot = "./thumbs" // 默认缩略图存储目录
	}

	t.regex = regexp.MustCompile(`^.*\/((\w)(\d+)x(\d+)(?:,([a-fA-F0-9]{6}))?(?:,q(\d+))?(?:,(\w+))?)\/(.+)$`)
	return nil
}

// Validate 验证配置
func (t *ThumbsServer) Validate() error {
	if t.ImageRoot == "" {
		return errors.New("image_root is required")
	}
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
	format := matches[7]
	imagePath := matches[8]

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
	thumbPath := filepath.Join(t.ThumbsRoot, modeDir, imagePath)
	originalPath := filepath.Join(t.ImageRoot, imagePath)
	
	// 检查缩略图是否已存在
	if _, err := os.Stat(thumbPath); err == nil {
		t.logger.Info("Serving existing thumbnail", zap.String("path", thumbPath))
		// 设置缓存头
		t.setCacheHeaders(w)
		http.ServeFile(w, r, thumbPath)
		return nil
	}

	// 检查原始图片是否存在
	if _, err := os.Stat(originalPath); os.IsNotExist(err) {
		t.logger.Error("Original image not found", zap.String("path", originalPath))
		return caddyhttp.Error(http.StatusNotFound, fmt.Errorf("original image not found: %s", imagePath))
	}

	// 创建缩略图目录
	thumbDir := filepath.Dir(thumbPath)
	if err := os.MkdirAll(thumbDir, 0755); err != nil {
		t.logger.Error("Failed to create thumbnail directory", zap.String("dir", thumbDir), zap.Error(err))
		return caddyhttp.Error(http.StatusInternalServerError, err)
	}

	// 根据模式生成缩略图
	var err error
	switch mode {
	case "m": // 保持纵横比，缩放到目标尺寸以内
		err = t.generateThumbnailModeM(originalPath, thumbPath, uint(width), uint(height), quality, format)
	case "c": // 保持纵横比，缩放到目标尺寸以内，然后从中心裁剪
		err = t.generateThumbnailModeC(originalPath, thumbPath, uint(width), uint(height), quality, format)
	case "w": // 保持纵横比，缩放到目标尺寸以内，然后将不足的部分填充为指定颜色
		err = t.generateThumbnailModeW(originalPath, thumbPath, uint(width), uint(height), bgColor, quality, format)
	case "f": // 先缩放再填充，确保完全填充目标区域
		err = t.generateThumbnailModeF(originalPath, thumbPath, uint(width), uint(height), bgColor, quality, format)
	default:
		err = fmt.Errorf("unsupported thumbnail mode: %s", mode)
	}

	if err != nil {
		t.logger.Error("Failed to generate thumbnail", zap.Error(err))
		return caddyhttp.Error(http.StatusInternalServerError, err)
	}

	t.logger.Info("Generated and served new thumbnail", 
		zap.String("path", thumbPath), 
		zap.String("mode", mode),
		zap.Int("quality", quality),
		zap.String("format", format))
	
	// 设置缓存头
	t.setCacheHeaders(w)
	http.ServeFile(w, r, thumbPath)
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

// generateThumbnailModeM 模式m：保持纵横比，缩放到目标尺寸以内
func (t ThumbsServer) generateThumbnailModeM(originalPath, thumbPath string, width, height uint, quality int, format string) error {
	// 打开原始图片文件
	file, err := os.Open(originalPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 解码图片
	img, err := t.decodeImage(file)
	if err != nil {
		return err
	}

	// 生成缩略图（保持纵横比）
	thumb := resize.Thumbnail(width, height, img, resize.Lanczos3)

	// 保存缩略图
	return t.encodeImage(thumb, thumbPath, originalPath, quality, format)
}

// generateThumbnailModeC 模式c：保持纵横比，缩放到目标尺寸以内，然后从中心裁剪
func (t ThumbsServer) generateThumbnailModeC(originalPath, thumbPath string, width, height uint, quality int, format string) error {
	// 打开原始图片文件
	file, err := os.Open(originalPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 解码图片
	img, err := t.decodeImage(file)
	if err != nil {
		return err
	}

	// 计算缩放比例，使至少一边等于目标尺寸
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

	// 从中心裁剪
	resizedBounds := resized.Bounds()
	x0 := (resizedBounds.Dx() - int(width)) / 2
	y0 := (resizedBounds.Dy() - int(height)) / 2
	x1 := x0 + int(width)
	y1 := y0 + int(height)

	// 确保裁剪区域在图片范围内
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > resizedBounds.Dx() {
		x1 = resizedBounds.Dx()
	}
	if y1 > resizedBounds.Dy() {
		y1 = resizedBounds.Dy()
	}

	cropped := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	draw.Draw(cropped, cropped.Bounds(), resized, image.Point{x0, y0}, draw.Src)

	// 保存缩略图
	return t.encodeImage(cropped, thumbPath, originalPath, quality, format)
}

// generateThumbnailModeW 模式w：保持纵横比，缩放到目标尺寸以内，然后将不足的部分填充为指定颜色
func (t ThumbsServer) generateThumbnailModeW(originalPath, thumbPath string, width, height uint, bgColor color.Color, quality int, format string) error {
	// 打开原始图片文件
	file, err := os.Open(originalPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 解码图片
	img, err := t.decodeImage(file)
	if err != nil {
		return err
	}

	// 生成缩略图（保持纵横比）
	resized := resize.Thumbnail(width, height, img, resize.Lanczos3)

	// 创建目标大小的画布
	canvas := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	
	// 填充背景色
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)
	
	// 计算居中位置
	resizedBounds := resized.Bounds()
	x := (int(width) - resizedBounds.Dx()) / 2
	y := (int(height) - resizedBounds.Dy()) / 2
	
	// 将缩略图绘制到画布上
	draw.Draw(canvas, image.Rect(x, y, x+resizedBounds.Dx(), y+resizedBounds.Dy()), resized, image.Point{}, draw.Over)

	// 保存缩略图
	return t.encodeImage(canvas, thumbPath, originalPath, quality, format)
}

// generateThumbnailModeF 模式f：先缩放再填充，确保完全填充目标区域
func (t ThumbsServer) generateThumbnailModeF(originalPath, thumbPath string, width, height uint, bgColor color.Color, quality int, format string) error {
	// 打开原始图片文件
	file, err := os.Open(originalPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 解码图片
	img, err := t.decodeImage(file)
	if err != nil {
		return err
	}

	// 计算缩放比例，使图片完全覆盖目标区域
	origBounds := img.Bounds()
	origWidth := uint(origBounds.Dx())
	origHeight := uint(origBounds.Dy())

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

	// 从中心裁剪
	resizedBounds := resized.Bounds()
	x0 := (resizedBounds.Dx() - int(width)) / 2
	y0 := (resizedBounds.Dy() - int(height)) / 2
	x1 := x0 + int(width)
	y1 := y0 + int(height)

	// 确保裁剪区域在图片范围内
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > resizedBounds.Dx() {
		x1 = resizedBounds.Dx()
	}
	if y1 > resizedBounds.Dy() {
		y1 = resizedBounds.Dy()
	}

	cropped := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	draw.Draw(cropped, cropped.Bounds(), resized, image.Point{x0, y0}, draw.Src)

	// 保存缩略图
	return t.encodeImage(cropped, thumbPath, originalPath, quality, format)
}

var (
	jpegHeader = []byte{0xFF, 0xD8}
	pngHeader  = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	webpHeader = []byte("RIFF")
	webpHeader2 = []byte("WEBP")
	avifHeader = []byte("ftyp")
)

// decodeImage 解码图片
func (t ThumbsServer) decodeImage(file *os.File) (image.Image, error) {
	var (
		buf = make([]byte, 16)
		numRead int
		err error
	)
	if numRead, err = file.Read(buf); err != nil{
		return nil, fmt.Errorf("failed to read file header: %v", err)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
        return nil, fmt.Errorf("failed to reset file position: %v", err)
    }
	
	if numRead >= 2 {
		switch  {
			case bytes.HasPrefix(buf, jpegHeader):
				return jpeg.Decode(file)
			case bytes.HasPrefix(buf, pngHeader):
				return png.Decode(file)
			case bytes.HasPrefix(buf, webpHeader):
				return t.decodeWebP(file)
			case bytes.HasPrefix(buf, webpHeader2):
				return t.decodeWebP(file)
			case bytes.HasPrefix(buf, avifHeader):
				return t.decodeAVIF(file)
			default:
				return nil, fmt.Errorf("unsupported image format!")
		}
	}
	return nil, fmt.Errorf("unsupported image format!")
}

// encodeImage 编码并保存图片
func (t ThumbsServer) encodeImage(img image.Image, path, originalPath string, quality int, format string) error {
	// 创建缩略图文件
	thumbFile, err := os.Create(path)
	if err != nil {
		return err
	}
	defer thumbFile.Close()

	// 确定输出格式
	outputFormat := strings.ToLower(filepath.Ext(originalPath))
	if format != "" {
		outputFormat = "." + format
	}
	
	// 根据格式保存图片
	switch outputFormat {
	case ".jpg", ".jpeg":
		return jpeg.Encode(thumbFile, img, &jpeg.Options{Quality: quality})
	case ".png":
		return png.Encode(thumbFile, img)
	case ".webp":
		return t.encodeWebP(thumbFile, img, quality)
	case ".avif":
		return t.encodeAVIF(thumbFile, img, quality)
	default:
		return fmt.Errorf("unsupported output format: %s", outputFormat)
	}
}

// decodeWebP 解码WebP图片
func (t ThumbsServer) decodeWebP(file *os.File) (image.Image, error) {
	// 这里使用一个假设的WebP解码库
	// 实际使用时需要引入真实的WebP解码库，如 github.com/chai2010/webp
	return nil, errors.New("WebP decoding not implemented")
}

// encodeWebP 编码WebP图片
func (t ThumbsServer) encodeWebP(file *os.File, img image.Image, quality int) error {
	// 这里使用一个假设的WebP编码库
	// 实际使用时需要引入真实的WebP编码库，如 github.com/chai2010/webp
	return errors.New("WebP encoding not implemented")
}

// decodeAVIF 解码AVIF图片
func (t ThumbsServer) decodeAVIF(file *os.File) (image.Image, error) {
	// 这里使用一个假设的AVIF解码库
	// 实际使用时需要引入真实的AVIF解码库，如 github.com/Kagami/go-avif
	return nil, errors.New("AVIF decoding not implemented")
}

// encodeAVIF 编码AVIF图片
func (t ThumbsServer) encodeAVIF(file *os.File, img image.Image, quality int) error {
	// 这里使用一个假设的AVIF编码库
	// 实际使用时需要引入真实的AVIF编码库，如 github.com/Kagami/go-avif
	return errors.New("AVIF encoding not implemented")
}

// parseHexColor 解析十六进制颜色代码
func parseHexColor(s string) (color.RGBA, error) {
	if len(s) != 6 {
		return color.RGBA{}, fmt.Errorf("invalid color format: %s", s)
	}
	
	r, err1 := strconv.ParseUint(s[0:2], 16, 8)
	g, err2 := strconv.ParseUint(s[2:4], 16, 8)
	b, err3 := strconv.ParseUint(s[4:6], 16, 8)
	
	if err1 != nil || err2 != nil || err3 != nil {
		return color.RGBA{}, fmt.Errorf("invalid color format: %s", s)
	}
	
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}, nil
}

// UnmarshalCaddyfile 解析Caddyfile配置
func (t *ThumbsServer) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "image_root":
				if !d.NextArg() {
					return d.ArgErr()
				}
				t.ImageRoot = d.Val()
			case "thumbs_root":
				if !d.NextArg() {
					return d.ArgErr()
				}
				t.ThumbsRoot = d.Val()
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

func main() {
	// 空的主函数，因为这是一个Caddy模块
}