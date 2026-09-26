# 轮次检测 sidecar 镜像（voice-turn-detection 决策 c）。模型文件不入镜像：
# 部署时把 scripts/turn-model/download.mjs 的落盘目录 volume 挂到
# TURN_MODEL_DIR（见 docker-compose.yml 的 turn-sidecar 服务）。
FROM python:3.11-slim

WORKDIR /app
# 先装依赖再拷代码，利用层缓存。
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY app.py .

ENV TURN_MODEL_DIR=/models/turn-detector \
    TURN_SIDECAR_PORT=8091
EXPOSE 8091
CMD ["python", "app.py"]
