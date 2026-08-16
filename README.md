# Delivery Order System (Cloud Spanner × PGAdapter)

Google Cloud 実践検証シリーズ【第172弾】のソースコード実体です。

Google Cloud の大規模分散 RDB「Cloud Spanner」（PostgreSQL Dialect / INTERLEAVE IN PARENT / UUIDv4 ホットスポット分散）× Cloud Run マルチコンテナ（PGAdapter UDS 通信）による、超高可用な出前注文システム検証環境です。

## 📖 詳しい解説・チュートリアル
本リポジトリの設計思想、ローカル検証（Docker Compose / Spanner Emulator + PGAdapter）、および Google Cloud 本番デプロイ手順の詳細解説は、Qiita および技術ブログにて公開しています：

👉 **Qiita 記事一覧**: [https://qiita.com/kuis](https://qiita.com/kuis)  
👉 **Author Blog**: [https://kuis.win](https://kuis.win)

---

## 🚀 クイックスタート (ローカル検証)

```bash
# コンテナ起動 (Spanner Emulator + PGAdapter + Go App)
docker compose up -d

# ブラウザでアクセス
open http://localhost:8080/
```

---

* 📜 **License**: MIT License (Copyright (c) 2026 kuiswin)
