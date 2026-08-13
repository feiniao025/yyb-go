增加web页面可查询api和重置api
默认账号密码为admin

## 部署
### docker (推荐)
```shell
docker run -d \
  --name yyb-go \
  --restart unless-stopped \
  -p 5800:5800 \
  ghcr.io/feiniao025/yyb-go:latest
```
