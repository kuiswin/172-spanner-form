# 1. プロジェクトおよびAPIの有効化
PROJECT_ID="your-google-cloud-project-id"
REGION="asia-northeast1"
SERVICE_NAME="spanner-delivery"
SA_NAME="spanner-client-sa"

gcloud config set project ${PROJECT_ID}

# 2. Spanner インスタンスとデータベースの作成 (PG-dialect)
gcloud spanner instances create main-spanner-instance     --config=regional-asia-northeast1     --description="Delivery Main Spanner Instance"     --processing-units=100 || true

gcloud spanner databases create delivery-db     --instance=main-spanner-instance     --database-dialect=POSTGRESQL || true

# 2.5. スキーマ (DDL) の適用
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "${SCRIPT_DIR}/schema.sql" ]; then
    gcloud spanner databases ddl update delivery-db --instance=main-spanner-instance --ddl-file="${SCRIPT_DIR}/schema.sql" || true
fi

# 3. Artifact Registry の作成
gcloud artifacts repositories create ${SERVICE_NAME}-repo     --repository-format=docker     --location=${REGION}     --description="Spanner App Docker repository" || true

# 4. 専用サービスアカウントの作成と最小権限の付与
SA_EMAIL="${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"
gcloud iam service-accounts create ${SA_NAME} --display-name="Spanner Delivery App SA" || true

# サービスアカウントへのSpannerデータベースユーザー権限の付与
gcloud spanner databases add-iam-policy-binding delivery-db     --instance=main-spanner-instance     --member="serviceAccount:${SA_EMAIL}"     --role="roles/spanner.databaseUser"
