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
- `usage`
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
- `usage`

可选包含：

- `brand_preference`
- `special_requirements`
- `notes`

### `price_catalog`

按以下类别分组：

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
- `price`
- `price_min`
- `price_max`
- `sample_count`

## 当前输出

当前输出是结构化推荐结果，至少包含：

- `summary`
- `parts`
- `total_price`
- `reasoning`
- `alternatives`
- `warnings`

`parts` 每项必须包含：

- `category`
- `model`
- `price`
- `reason`

## 当前接口

- `GET /healthz`
- `GET /api/v1/catalog/prices`
- `POST /api/v1/advice/catalog`

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
