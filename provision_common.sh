# 1. プロジェクトおよびAPIの有効化
PROJECT_ID="your-google-cloud-project-id"
REGION="asia-northeast1"
SERVICE_NAME="spanner-delivery"
SA_NAME="spanner-client-sa"

gcloud config set project ${PROJECT_ID}

# -----------------------------------------------------------------
# ⚙️ 【モード選択】
#   MODE="safe"   : 🛡️ 90日間完全無料！東京 100 PU 安心モード
#   MODE="roman"  : 🔥 【極大ロマン】地球3大陸マルチリージョン (nam-eur-asia1 / 1,000 PU)
# -----------------------------------------------------------------
MODE="roman"

if [ "$MODE" = "roman" ]; then
    echo "🔥 【極大ロマンモード発動】地球3大陸マルチリージョン (nam-eur-asia1 / 1,000 PU) を召喚します！"
    echo "⚠️ ※ 約90円の10分間限定体験です。遊んだ後は必ず teardown.sh で解約してください！"
    CONFIG_FLAGS="--config=nam-eur-asia1 --nodes=1"
else
    echo "🛡️ 【安心モード発動】90日間完全無料の東京 100 PU で作成します！"
    CONFIG_FLAGS="--config=regional-asia-northeast1 --processing-units=100"
fi

# 2. Spanner インスタンスとデータベースの作成 (PG-dialect)
gcloud spanner instances create main-spanner-instance ${CONFIG_FLAGS} --description="Delivery Main Spanner Instance" || true

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
