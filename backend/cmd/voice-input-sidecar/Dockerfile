# 用户语音情绪 sidecar 镜像（openspec/changes/user-voice-affect 波1）。
# 模型文件不入镜像：部署时把 scripts/voice-model/download.mjs 的落盘目录
# volume 挂到 VOICE_MODEL_DIR（见 docker-compose.yml 的 voice-input-sidecar
# 服务）。
FROM python:3.11-slim

WORKDIR /app
# 先装依赖再拷代码，利用层缓存。
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY app.py .

ENV VOICE_MODEL_DIR=/models/voice \
    VOICE_SIDECAR_PORT=8092
EXPOSE 8092
CMD ["python", "app.py"]
