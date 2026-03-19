# rigel-build-engine

`rigel-build-engine` 是当前系统的核心分析服务。

## 当前职责

- 接收来自界面的用户参数
- 读取京东原始硬件数据
- 按型号词库整理出型号级价格清单
- 构造 AI 输入
- 请求 AI API
- 返回结构化推荐结果

## 不负责什么

- 不直接抓取京东或其他平台
- 不承担前端页面
- 当前不做复杂规则引擎

## 当前输入

### 1. 用户需求

- `budget`
- `use_case`
- `build_mode`
- `brand_preference`
- `special_requirements`
- `notes`

### 2. 当前硬件价格信息

来源于 `rigel-jd-collector` 已入库的原始商品。

build-engine 会将这些原始商品整理成型号级价格清单，再作为 AI 输入的一部分。

## 当前 AI 输入规范

### `user_request`

必须包含：

- `budget`
- `use_case`

可选包含：

- `build_mode`
- `brand_preference`
- `special_requirements`
- `notes`

### `price_catalog`

这里的 `price_catalog` 示例按类别分组展示，目的是让 AI 输入结构更直观。

注意：

- 这里是 AI 输入说明性示例
- 不等于当前 `POST /api/v1/advice/catalog` 的实际 HTTP 请求体结构
- 当前接口实际仍使用 `catalog.items` 扁平数组，每个条目带 `category`

`category` 当前允许值：

- `cpu`
- `gpu`
- `motherboard`
- `ram`
- `ssd`
- `psu`
- `case`
- `cooler`

每个型号对象包含：

- `model`
- `display_name`
- `avg_price`
- `median_price`
- `price_min`
- `price_max`
- `sample_count`

说明性示例如下：

```json
{
  "price_catalog": {
    "cpu": [
      {
        "category": "CPU",
        "model": "7500f",
        "display_name": "AMD 7500f",
        "avg_price": 899,
        "median_price": 899,
        "min_price": 859,
        "max_price": 939,
        "sample_count": 3
      }
    ],
    "gpu": [
      {
        "category": "GPU",
        "model": "rtx 4060",
        "display_name": "NVIDIA rtx 4060",
        "avg_price": 2399,
        "median_price": 2399,
        "min_price": 2299,
        "max_price": 2499,
        "sample_count": 4
      }
    ],
    "motherboard": [],
    "ram": [],
    "ssd": [],
    "psu": [],
    "case": [],
    "cooler": []
  }
}
```

## 当前输出

当前输出是结构化推荐结果，至少包含：

- `provider`
- `fallback_used`
- `selection`
- `advisory`

`selection` 至少包含：

- `budget`
- `use_case`
- `build_mode`
- `estimated_total`
- `selected_items`

`selected_items` 每项至少包含：

- `category`
- `display_name`
- `selected_price`
- `reasons`

## 当前接口

- `GET /healthz`
- `GET /api/v1/catalog/prices`
- `POST /api/v1/advice/catalog`

## 配置方式

当前服务默认读取：

```text
configs/config.yaml
```

启动示例：

```bash
go run ./cmd/server -config ./configs/config.yaml
```

## 接口示例

### 1. 健康检查

请求：

```bash
curl http://localhost:18082/healthz
```

响应示例：

```json
{
  "status": "ok",
  "service": "rigel-build-engine",
  "mode": "local"
}
```

### 2. 获取型号级价格目录

请求：

```bash
curl "http://localhost:18082/api/v1/catalog/prices?use_case=gaming&build_mode=mixed&limit=20"
```

响应示例：

```json
{
  "use_case": "gaming",
  "build_mode": "mixed",
  "warnings": [],
  "items": [
    {
      "category": "CPU",
      "brand": "AMD",
      "model": "7500f",
      "display_name": "AMD 7500f",
      "normalized_key": "cpu-7500f",
      "sample_count": 3,
      "avg_price": 899,
      "median_price": 899,
      "min_price": 859,
      "max_price": 939,
      "platforms": ["jd"],
      "source_breakdown": [
        {
          "source_platform": "jd",
          "sample_count": 3,
          "avg_price": 899,
          "min_price": 859,
          "max_price": 939
        }
      ]
    },
    {
      "category": "GPU",
      "brand": "NVIDIA",
      "model": "rtx 4060",
      "display_name": "NVIDIA rtx 4060",
      "normalized_key": "gpu-rtx-4060",
      "sample_count": 4,
      "avg_price": 2399,
      "median_price": 2399,
      "min_price": 2299,
      "max_price": 2499,
      "platforms": ["jd"],
      "source_breakdown": [
        {
          "source_platform": "jd",
          "sample_count": 4,
          "avg_price": 2399,
          "min_price": 2299,
          "max_price": 2499
        }
      ]
    }
  ]
}
```

说明：

- `use_case` 当前支持：`gaming`、`office`、`design`
- `build_mode` 当前支持：`new_only`、`used_only`、`mixed`
- `limit` 是读取原始商品时的上限，不是最终目录项数量

### 3. 根据价格目录生成推荐草案

请求：

```bash
curl -X POST http://localhost:18082/api/v1/advice/catalog \
  -H "Content-Type: application/json" \
  -d '{
    "budget": 6000,
    "use_case": "gaming",
    "build_mode": "mixed",
    "catalog": {
      "use_case": "gaming",
      "build_mode": "mixed",
      "items": [
        {
          "category": "CPU",
          "brand": "AMD",
          "model": "7500f",
          "display_name": "AMD 7500f",
          "normalized_key": "cpu-7500f",
          "sample_count": 3,
          "avg_price": 899,
          "median_price": 899,
          "min_price": 859,
          "max_price": 939,
          "platforms": ["jd"]
        },
        {
          "category": "GPU",
          "brand": "NVIDIA",
          "model": "rtx 4060",
          "display_name": "NVIDIA rtx 4060",
          "normalized_key": "gpu-rtx-4060",
          "sample_count": 4,
          "avg_price": 2399,
          "median_price": 2399,
          "min_price": 2299,
          "max_price": 2499,
          "platforms": ["jd"]
        },
        {
          "category": "RAM",
          "brand": "Gloway",
          "model": "ddr5 6000 32g",
          "display_name": "Gloway ddr5 6000 32g",
          "normalized_key": "ram-ddr5-6000-32g",
          "sample_count": 2,
          "avg_price": 509,
          "median_price": 509,
          "min_price": 459,
          "max_price": 559,
          "platforms": ["jd"]
        },
        {
          "category": "SSD",
          "brand": "WD",
          "model": "sn770 1tb",
          "display_name": "WD sn770 1tb",
          "normalized_key": "ssd-sn770-1tb",
          "sample_count": 2,
          "avg_price": 399,
          "median_price": 399,
          "min_price": 379,
          "max_price": 419,
          "platforms": ["jd"]
        }
      ]
    }
  }'
```

响应示例：

```json
{
  "provider": "local",
  "fallback_used": true,
  "selection": {
    "budget": 6000,
    "use_case": "gaming",
    "build_mode": "mixed",
    "estimated_total": 4206,
    "warnings": [
      "当前价格目录缺少这些类别的数据：MB、PSU、CASE、COOLER。"
    ],
    "selected_items": [
      {
        "category": "CPU",
        "display_name": "AMD 7500f",
        "normalized_key": "cpu-7500f",
        "sample_count": 3,
        "selected_price": 899,
        "median_price": 899,
        "source_platforms": ["jd"],
        "reasons": [
          "当前类别按 1200 元目标预算挑选了更接近中位价的型号。",
          "已参考 3 个价格样本。"
        ]
      },
      {
        "category": "GPU",
        "display_name": "NVIDIA rtx 4060",
        "normalized_key": "gpu-rtx-4060",
        "sample_count": 4,
        "selected_price": 2399,
        "median_price": 2399,
        "source_platforms": ["jd"],
        "reasons": [
          "当前类别按 3000 元目标预算挑选了更接近中位价的型号。",
          "已参考 4 个价格样本。"
        ]
      }
    ]
  },
  "advisory": {
    "summary": "基于当前价格目录，这份 gaming 采购草案总价约 4206 元，核心组合为 AMD 7500f 和 NVIDIA rtx 4060。",
    "reasons": [
      "本次按 6000 元预算和 gaming 用途，从当前价格目录中挑选了更接近预算中心的型号。",
      "草案总价约 4206 元，优先参考了各型号的中位价和样本量。",
      "build-engine 已整理当前硬件信息并生成 AI 分析草案。",
      "核心组合当前倾向于 AMD 7500f + NVIDIA rtx 4060。"
    ],
    "fit_for": [
      "1080p/2K 主流游戏场景",
      "以 AMD 7500f + NVIDIA rtx 4060 为核心的均衡游戏平台"
    ],
    "risks": [
      "当前价格目录缺少这些类别的数据：MB、PSU、CASE、COOLER。",
      "价格目录会随平台活动和库存变化波动，建议下单前重新抓取一次最新价格。",
      "当前仍是本地模板化分析路径，真实外部 AI API 尚未接入。"
    ],
    "upgrade_advice": [
      "如果游戏库会持续变大，优先把 SSD 升到 2TB。",
      "预算仍有余量时，可以先把显卡或 CPU 提升一个档位，再复核整机兼容性。"
    ],
    "alternative_note": "如果你更看重品牌、静音或不同采购偏好，可以在同一份价格目录上再生成一版草案。"
  }
}
```

说明：

- 当前 `provider` 可能是本地占位实现，例如 `local`
- 当前 `fallback_used=true` 代表仍走模板化推荐路径，不代表已接入真实外部 AI
- 如果 `catalog.items` 为空，接口会返回 `400`

## 当前重点

当前重点不是复杂兼容规则。
当前重点是：

1. 把原始商品整理成 AI 可用的型号级价格清单
2. 把 `用户需求 + 价格清单` 稳定交给 AI
3. 返回统一格式的推荐结果

## TODO / MOCK

- `TODO`: 接入真实外部 AI API
- `TODO`: 继续收紧型号归一规则
- `TODO`: 与 `rigel_keyword_seeds` 形成稳定映射关系
- `MOCK`: 当前仍可保留本地模板化分析作为过渡实现
