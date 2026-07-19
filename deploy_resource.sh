# Cloud Runへのソースベースデプロイ（マルチコンテナ・サイドカー構成）
# ※gcloud run deploy --source . はCloud Buildでのコンテナビルドとデプロイを自動的に一括で行います。
gcloud run deploy ${SERVICE_NAME} \
    --source . \
    --region ${REGION} \
    --allow-unauthenticated \
    --service-account=${SA_EMAIL} \
    --container app \
      --set-env-vars="DATABASE_URL=postgres://127.0.0.1:5432/delivery-db?sslmode=disable" \
      --port 80 \
    --container pgadapter \
      --image gcr.io/cloud-spanner-pg-adapter/pgadapter:latest \
      --args="-p,${PROJECT_ID},-i,main-spanner-instance" \
    --max-instances 10
