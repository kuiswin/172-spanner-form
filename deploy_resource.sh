# 作業ディレクトリへの移動
cd /root/QIITAMD/sandbox_172 2>/dev/null || cd /root/sandbox_172 2>/dev/null || true
# ※gcloud run deploy --source . はCloud Buildでのコンテナビルドとデプロイを自動的に一括で行います。
gcloud run deploy ${SERVICE_NAME} \
    --region ${REGION} \
    --allow-unauthenticated \
    --service-account=${SA_EMAIL} \
    --execution-environment=gen1 \
    --max-instances 10 \
    --quiet \
    --add-volume=name=sockets-dir,type=in-memory,size-limit=50Mi \
    --container pgadapter \
      --image="gcr.io/cloud-spanner-pg-adapter/pgadapter:latest" \
      --args="-p,${PROJECT_ID},-i,main-spanner-instance,-dir,/sockets,-s,5432,-hh,0.0.0.0" \
      --add-volume-mount=volume=sockets-dir,mount-path=/sockets \
      --startup-probe=tcpSocket.port=5432,initialDelaySeconds=10,timeoutSeconds=5,failureThreshold=10 \
    --container app \
      --source . \
      --depends-on=pgadapter \
      --set-env-vars="DATABASE_URL=host=/sockets port=5432 user=postgres dbname=delivery-db sslmode=disable" \
      --port 80 \
      --add-volume-mount=volume=sockets-dir,mount-path=/sockets
