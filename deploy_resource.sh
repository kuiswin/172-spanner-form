# Cloud Runへのソースベースデプロイ（マルチコンテナ・サイドカー構成）
# ※gcloud run deploy --source . はCloud Buildでのコンテナビルドとデプロイを自動的に一括で行います。
gcloud run deploy ${SERVICE_NAME} \
    --source . \
    --region ${REGION} \
    --allow-unauthenticated \
    --service-account=${SA_EMAIL} \
    --execution-environment=gen1 \
    --volume=name=sockets-dir,type=in-memory,size=50Mi \
    --container pgadapter \
      --image="gcr.io/cloud-spanner-pg-adapter/pgadapter:latest" \
      --args="-p,${PROJECT_ID},-i,main-spanner-instance,-dir,/sockets" \
      --volume-mount=name=sockets-dir,mount-path=/sockets \
    --container app \
      --depends-on=pgadapter \
      --set-env-vars="DATABASE_URL=host=/sockets port=5432 user=postgres dbname=delivery-db sslmode=disable" \
      --port 80 \
      --volume-mount=name=sockets-dir,mount-path=/sockets \
    --max-instances 10
