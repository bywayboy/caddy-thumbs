## 项目说明:

这是一个Caddy的缩略图生成插件. 它实现了几种缩放模式.

| 缩放模式 | 说明 |
|-------|-------|
| m | 保持比例缩放 |
| c | 保持比例并裁剪 |
| w | 保持比例并填充 |
| f  | 填充并裁剪模式 |


## 配置演示

### 基本配置
```caddyfile
site.com {
     root * /data/www
     route /thumbs/* {
          thumbs_server {
               image_root /data/www/images
               thumbs_root /data/www/thumbs
        }
     }
}
```

### 完整配置示例
```
site.com {
     root * /data/www
     route /thumbs/* {
          thumbs_server {
               image_root /data/www/images
               thumbs_root /data/www/thumbs
               max_dimension 2000
               default_quality 90
               cache_control "public, max-age=31536000, immutable"
          }
     }
}
```

### 使用相对路径的配置

```caddyfile
site.com {
   root * /data/www
   route /thumbs/* {
        thumbs_server {
             image_root ./images
             thumbs_root ./thumbs
        }
   }
}
```
## 使用示例

现在您可以使用新的 thumbs_root 配置来指定缩略图的存储目录：

1. `https://site.com/thumbs/m100x100/image.jpg` - 缩略图将保存在 /data/www/thumbs/m100x100/image.jpg
2. `https://site.com/thumbs/c200x200,q85/image.jpg` - 缩略图将保存在 /data/www/thumbs/c200x200,q85/image.jpg
3. `https://site.com/thumbs/w300x300,ff0000/image.jpg` - 缩略图将保存在 /data/www/thumbs/w300x300,ff0000/image.jpg

