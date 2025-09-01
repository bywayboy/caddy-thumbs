## 项目说明:

[简体中文](./README_CN.md) | [English](./README.md)

这是一个运行与 Caddy2 上的缩略图生成插件. 它实现了几种缩放模式, 通过存储引擎插件支持多种存储方式.


# 编译方法

第一步 安装依赖包
```shell
# Ubuntu/Debian
sudo apt-get install libwebp-dev

# CentOS/RHEL
sudo yum install libwebp-devel

# macOS
brew install webp
```

第二步 安装开始编译
```
export CGO_CFLAGS="-I/usr/local/include"
export CGO_LDFLAGS="-L/usr/local/lib -lwebp"
export CGO_ENABLED=1
# 使用xcaddy 编译
xcaddy build --with github.com/caddy-dns/alidns --with git.exti.cc/bywayboy/caddy-thumbs=./caddy-thumbs
```

## 使用方式

URL格式: `https://site.com/<prefix>/{mode}{width}x{height},{param}/{image_path}`

| 缩放模式 | 说明 |
|-------|-------|
| m | 保持纵横比，缩放到目标尺寸以内（可能不是 exactly 目标尺寸） |
| wlt,wlc,wlb,wrt,wrc,wrb,wcc或w | 缩放到目标尺寸以内，图片居左上、左中、左下，右上、右中，右下，中中。然后将不足的部分填充为指定颜色（exactly 目标尺寸） |
| lt,lc,lb,rt,rc,rb,c | 左上、左中、左下，右上、右中，右下，中中 对齐缩放剪裁。(exactly 目标尺寸) |

## param 是可选的，格式为 `{color},q{quality}`

quality 质量参数, q1-q100, 默认为 q90
color 填充颜色, 格式为 #RRGGBB, 默认为 #FFFFFF


## 配置演示

### 基本配置
```caddyfile
site.com {
     root * /data/www
     route /thumbs/* {
          thumbs_server {
               thumbs_storage file_system {
                    root /data/wwwroot/fserver/public/thumbs
               }
               image_storage file_system {
                    root /data/wwwroot/fserver/public/images
               }
        }
     }
}
```

### 完整配置示例
```caddyfile
site.com {
     root * /data/www
     route /thumbs/* {
          thumbs_server {
               thumbs_storage file_system {
                    root /data/wwwroot/fserver/public/thumbs
               }
               image_storage file_system {
                    root /data/wwwroot/fserver/public/images
               }
               max_dimension 2000
               default_quality 90
               cache_control "public, max-age=31536000, immutable"
          }
     }
}
```


现在您可以使用新的 thumbs_root 配置来指定缩略图的存储目录：

1. `https://site.com/thumbs/m100x100/image.jpg` - 缩略图将保存在 /data/www/thumbs/m100x100/image.jpg
2. `https://site.com/thumbs/c200x200,q85/image.jpg` - 缩略图将保存在 /data/www/thumbs/c200x200,q85/image.jpg
3. `https://site.com/thumbs/w300x300,ff0000/image.jpg` - 缩略图将保存在 /data/www/thumbs/w300x300,ff0000/image.jpg
4. `https://site.com/thumbs/f400x400,ff0000,q90/image.jpg` - 缩略图将保存在 /data/www/thumbs/f400x400,ff0000,q90/image.jpg

## 思考

是否可以考虑使用 singleflight 来避免重复生成缩略图?