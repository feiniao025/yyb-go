增加web页面可查询api和重置api
默认账号密码为admin

## 部署
### docker 
```shell
docker run -d \
  --name yyb-go \
  --restart unless-stopped \
  -p 5800:5800 \
  -v /root/yyb/yyb_db:/app/resource/db \
  -v /root/yyb/yyb_avatars:/app/resource/avatars \
  -v /root/yyb/yyb_qr:/app/resource/qr \
  ghcr.io/feiniao025/yyb-go:latest
```
