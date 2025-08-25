## 项目说明:

这是一个Caddy的缩略图生成插件. 它实现了几种缩放模式, 通过存储引擎插件支持多种存储方式.

| 缩放模式 | 说明 |
|-------|-------|
| m | 保持纵横比，缩放到目标尺寸以内（可能不是 exactly 目标尺寸） |
| c | 缩放到目标尺寸以内，然后从中心裁剪（exactly 目标尺寸） |
| w | 缩放到目标尺寸以内，然后将不足的部分填充为指定颜色（exactly 目标尺寸） |
| f  | 填充并裁剪模式,先缩放，然后超出的地方居中剪裁。(exactly 目标尺寸) |


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

## 使用示例

现在您可以使用新的 thumbs_root 配置来指定缩略图的存储目录：

1. `https://site.com/thumbs/m100x100/image.jpg` - 缩略图将保存在 /data/www/thumbs/m100x100/image.jpg
2. `https://site.com/thumbs/c200x200,q85/image.jpg` - 缩略图将保存在 /data/www/thumbs/c200x200,q85/image.jpg
3. `https://site.com/thumbs/w300x300,ff0000/image.jpg` - 缩略图将保存在 /data/www/thumbs/w300x300,ff0000/image.jpg

