## 项目说明:

```caddyfile
site.com {
   root * /data/www
   handle /thumbs/* {
        thumbs_server {
             image_root /data/www/images
        }
   }
}
```