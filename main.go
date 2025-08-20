package caddy_thumbs

import (
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

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
	ImageRoot string `json:"image_root,omitempty"`
	logger    *zap.Logger
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
	return nil
}

// Validate 验证配置
func (t *ThumbsServer) Validate() error {
	if t.ImageRoot == "" {
		return errors.New("image_root is required")
	}
	return nil
}

// ServeHTTP 处理HTTP请求
func (t ThumbsServer) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// 解析请求路径，提取尺寸信息和原始图片路径
	path := r.URL.Path
	re := regexp.MustCompile(`^/thumbs/(m(\d+)x(\d+))/(.+)$`)
	matches := re.FindStringSubmatch(path)

	if len(matches) != 5 {
		return caddyhttp.Error(http.StatusNotFound, errors.New("invalid thumbnail request format"))
	}

	sizeDir := matches[1]
	width, _ := strconv.Atoi(matches[2])
	height, _ := strconv.Atoi(matches[3])
	imagePath := matches[4]

	// 构建缩略图路径和原始图片路径
	thumbPath := filepath.Join(".", "thumbs", sizeDir, imagePath)
	originalPath := filepath.Join(t.ImageRoot, imagePath)

	// 检查缩略图是否已存在
	if _, err := os.Stat(thumbPath); err == nil {
		t.logger.Info("Serving existing thumbnail", zap.String("path", thumbPath))
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

	// 生成缩略图
	if err := t.generateThumbnail(originalPath, thumbPath, uint(width), uint(height)); err != nil {
		t.logger.Error("Failed to generate thumbnail", zap.Error(err))
		return caddyhttp.Error(http.StatusInternalServerError, err)
	}

	t.logger.Info("Generated and served new thumbnail", zap.String("path", thumbPath))
	http.ServeFile(w, r, thumbPath)
	return nil
}

// generateThumbnail 生成缩略图
func (t ThumbsServer) generateThumbnail(originalPath, thumbPath string, width, height uint) error {
	// 打开原始图片文件
	file, err := os.Open(originalPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 解码图片
	var img image.Image
	ext := strings.ToLower(filepath.Ext(originalPath))

	switch ext {
	case ".jpg", ".jpeg":
		img, err = jpeg.Decode(file)
	case ".png":
		img, err = png.Decode(file)
	default:
		return fmt.Errorf("unsupported image format: %s", ext)
	}

	if err != nil {
		return err
	}

	// 生成缩略图
	thumb := resize.Thumbnail(width, height, img, resize.Lanczos3)

	// 创建缩略图文件
	thumbFile, err := os.Create(thumbPath)
	if err != nil {
		return err
	}
	defer thumbFile.Close()

	// 编码并保存缩略图
	switch ext {
	case ".jpg", ".jpeg":
		err = jpeg.Encode(thumbFile, thumb, nil)
	case ".png":
		err = png.Encode(thumbFile, thumb)
	}

	return err
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
