## Description:

[简体中文](./README_CN.md)
[English](./README.md)

[简体中文]
This is a thumbnail generation plugin running on Caddy2. It implements several scaling modes and supports multiple storage methods through storage plugins.


## Compilation Method

Step 1: Install Dependencies

```bash
# Ubuntu/Debian
 apt-get install libwebp-dev    
 # CentOS/RHEL   
sudo yum install libwebp-devel    
# macOS   
brew install webp   
```
Step 2: Start Compilation

```bash
export CGO_CFLAGS="-I/usr/local/include"
export CGO_LDFLAGS="-L/usr/local/lib -lwebp"
export CGO_ENABLED=1
# Using xcaddy for compilation
xcaddy build --with github.com/caddy-dns/alidns --with git.exti.cc/bywayboy/caddy-thumbs=./caddy-thumbs   
```


This is a Caddy plugin for thumbnail generation. It implements several scaling modes and supports multiple storage methods through storage engine plugins.





| Scaling Mode | Description |
|-------|-------|
| m | Maintains aspect ratio, scales within target dimensions (may not be exactly target size) |
| c | Scales within target dimensions, then crops from center (exactly target size) |
| w | Scales within target dimensions, then fills remaining area with specified color (exactly target size) |
| f | Fill-and-crop mode: scales first, then center-crops excess area (exactly target size) |


## Configuration Demo

### Basic Configuration
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

### Complete Configuration Example

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

## Usage Examples

You can now use the new thumbs_root configuration to specify the thumbnail storage directory:

1. https://site.com/thumbs/m100x100/image.jpg - Thumbnail saved at /data/www/thumbs/m100x100/image.jpg
2. https://site.com/thumbs/c200x200,q85/image.jpg - Thumbnail saved at /data/www/thumbs/c200x200,q85/image.jpg
3. https://site.com/thumbs/w300x300,ff0000/image.jpg - Thumbnail saved at /data/www/thumbs/w300x300,ff0000/image.jpg

