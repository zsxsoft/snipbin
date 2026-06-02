# Snipbin

轻量级临时文件与文本分享服务，支持 Web 界面与命令行上传，自动过期清理。

## 功能特性

- **双模式上传**：支持 `curl` 命令行直传和 Web 表单上传
- **自动过期**：可自定义 TTL（1 分钟 ~ 24 小时），默认 1 小时
- **短链接分享**：3 位 nanoid 风格短 ID，方便传播
- **智能存储**：小于 1MB 的文件优先写入 `/dev/shm`（内存），大文件落盘 `/tmp`
- **重启恢复**：启动时自动扫描并恢复未过期的分享内容
- **嵌入式 Web UI**：前端资源嵌入二进制，单文件即可运行

## 快速开始

### 本地运行

服务默认监听 `:8080`，可通过环境变量调整：

```bash
LISTEN_ADDR=:3000 BASE_URL=https://snip.example.com go run main.go
```

### Docker 运行

```bash
docker run -d -p 8080:8080 --name snipbin ghcr.io/zsxsoft/snipbin:main
```

#### 本地构建

```bash
docker build -t snipbin .
```

## 使用方式

### 命令行上传（curl）

```bash
# 上传文本
echo "hello world" | curl --data-binary @- http://localhost:8080

# 上传文件
curl --data-binary @photo.jpg http://localhost:8080

# 自定义过期时间
curl --data-binary @doc.pdf -H "X-TTL: 30m" http://localhost:8080

# 自定义文件名
curl --data-binary @data.bin -H "X-Filename: backup.bin" http://localhost:8080

# JSON 格式返回
curl --data-binary @file.txt "http://localhost:8080?format=json"
```

返回分享链接，例如：`http://localhost:8080/s/abc`

### Web 界面上传

打开 `http://localhost:8080`，直接拖拽文件或粘贴文本即可。

### 下载分享内容

```bash
curl http://localhost:8080/s/abc -o file.txt
```

浏览器中直接访问 `/s/{id}`，支持预览的图片/文本会直接显示，其他类型自动下载。

### 删除分享内容

```bash
curl -X DELETE http://localhost:8080/s/abc
```

## 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `LISTEN_ADDR` | 监听地址 | `:8080` |
| `BASE_URL` | 分享链接的基础 URL（自动推断） | 空 |

## API 速查

| 方法 | 路径 | 说明 |
|------|------|------|
| `PUT` | `/` | 上传内容（body 为文件/文本数据） |
| `POST` | `/` | 表单上传（`file` 或 `text` 字段） |
| `GET` | `/s/{id}` | 下载/预览分享内容 |
| `DELETE` | `/s/{id}` | 删除分享内容 |
| `GET` | `/api/recent` | 获取最近 50 条分享记录 |
| `GET` | `/healthz` | 健康检查 |

## 上传参数

| 参数 | 位置 | 说明 |
|------|------|------|
| `X-TTL` / `?ttl=` / 表单 `ttl` | Header / Query / Form | 过期时间，如 `10m`、`1h30m`、`3600`（秒） |
| `X-Filename` / `Content-Disposition` | Header | 自定义文件名 |
| `?format=json` | Query | 返回 JSON 格式（含 `id`、`url`、`expiresAt`） |

## 开源协议

The MIT License
