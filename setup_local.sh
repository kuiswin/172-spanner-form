# 依存ツールのインストール (未導入の場合)
sudo apt-get update && sudo apt-get install -y docker.io docker-compose-v2 git postgresql-client

# リポジトリのクローンと作業ディレクトリへの移動
git clone https://github.com/kuiswin/172-spanner-form.git sandbox_172
cd sandbox_172

# コンテナのビルドとバックグラウンド起動
docker compose up -d --build
