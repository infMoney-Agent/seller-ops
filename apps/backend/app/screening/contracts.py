from datetime import date
from typing import Literal
from uuid import UUID
from pydantic import BaseModel, ConfigDict, Field, model_validator
from .domain import PROFIT_FIELDS, calculate, validate_policy


class Strict(BaseModel):
    model_config = ConfigDict(extra="forbid")


class Review(Strict):
    expected_review_revision: int = Field(ge=0)
    expected_data_revision: int = Field(ge=0)
    status: Literal["kept", "excluded", "unsure"]
    reason: str = Field(default="", max_length=2000)
    note: str = Field(default="", max_length=10000)

    @model_validator(mode="after")
    def reason_required(self):
        if self.status in ("excluded", "unsure") and not self.reason.strip():
            raise ValueError("排除或待确认必须填写原因")
        return self


class Batch(Strict):
    product_ids: list[UUID] = Field(min_length=1, max_length=100)
    mode: Literal["direct", "reference"] = "direct"
    reference_product_id: UUID | None = None
    status: Literal["kept", "excluded", "unsure"] = "kept"
    reason: str = Field(default="", max_length=2000)
    note: str = Field(default="", max_length=10000)

    @model_validator(mode="after")
    def check(self):
        if len(set(self.product_ids)) != len(self.product_ids):
            raise ValueError("重复商品ID")
        if self.mode == "reference" and not self.reference_product_id:
            raise ValueError("请选择参考样本")
        if (
            self.mode == "direct"
            and self.status in ("excluded", "unsure")
            and not self.reason.strip()
        ):
            raise ValueError("请填写原因")
        return self


class PeriodMetadata(Strict):
    source_type: str = Field(
        default="sellersprite", min_length=1, max_length=50, pattern=r"^[a-zA-Z0-9_-]+$"
    )
    period_type: Literal["calendar_month", "rolling_30d", "custom"] = "calendar_month"
    period_start: date | None = None
    period_end: date | None = None


class Mapping(PeriodMetadata):
    mapping: dict[str, str] = Field(default_factory=dict)
    observed_on: date | None = None
    source_period: str = Field(default="", max_length=80)

    @model_validator(mode="after")
    def check(self):
        from .domain import ALIASES
        from .monthly import period_metadata

        bounds = period_metadata(
            self.observed_on,
            self.source_period,
            self.period_type,
            self.period_start,
            self.period_end,
        )
        self.period_start = bounds["period_start"]
        self.period_end = bounds["period_end"]

        if set(self.mapping) - set(ALIASES):
            raise ValueError("未知映射字段")
        if len(self.mapping) > 30 or any(len(v) > 200 for v in self.mapping.values()):
            raise ValueError("字段映射超限")
        return self


class Policy(Strict):
    name: str = Field(min_length=1, max_length=100)
    config: dict

    @model_validator(mode="after")
    def check(self):
        validate_policy(self.config)
        return self


class Selection(Strict):
    product_ids: list[UUID] | None = Field(default=None, max_length=100000)
    filters: dict = Field(default_factory=dict)


class ProfitInputs(Strict):
    name: str = Field(default="基准", min_length=1, max_length=100)
    sale_price_usd: str | None = None
    discount_usd: str | None = None
    purchase_cny: str | None = None
    packaging_cny: str | None = None
    freight_cny: str | None = None
    other_landed_cny: str | None = None
    fx_cny_per_usd: str | None = None
    commission_rate: str | None = None
    fba_fee_usd: str | None = None
    storage_usd: str | None = None
    ad_rate: str | None = None
    return_rate: str | None = None
    loss_per_return_usd: str | None = None
    other_usd: str | None = None
    monthly_units: int | None = Field(default=None, ge=0, le=1000000000)
    fee_source: str = Field(default="", max_length=2000)
    note: str = Field(default="", max_length=2000)

    @model_validator(mode="after")
    def check(self):
        from decimal import Decimal, InvalidOperation

        for key in PROFIT_FIELDS:
            raw = getattr(self, key)
            if raw in (None, ""):
                continue
            try:
                n = Decimal(str(raw))
            except InvalidOperation:
                raise ValueError(f"{key} 数值无效")
            if not n.is_finite() or n < 0 or n > Decimal("999999999999"):
                raise ValueError(f"{key} 数值无效")
            if key.endswith("_rate") and n > 1:
                raise ValueError(f"{key} 必须在0到1之间")
            if key == "fx_cny_per_usd" and n <= 0:
                raise ValueError("汇率必须大于零")
        calculate(self.model_dump())
        return self


class MonthlyReference(Strict):
    monthly_id: UUID | None = None
    expected_monthly_revision: int | None = Field(default=None, ge=1)

    @model_validator(mode="after")
    def monthly_reference_pair(self):
        if (self.monthly_id is None) != (self.expected_monthly_revision is None):
            raise ValueError("月度记录ID与版本必须一起提供")
        return self


class ProfitRequest(MonthlyReference):
    product_id: UUID
    expected_data_revision: int = Field(ge=0)
    scenarios: list[ProfitInputs] = Field(min_length=1, max_length=20)


class SaveProfit(MonthlyReference):
    expected_data_revision: int = Field(ge=0)
    inputs: ProfitInputs
    template_id: UUID | None = None


class Template(Strict):
    name: str = Field(min_length=1, max_length=100)
    params: ProfitInputs


class ProfitBatch(Selection):
    inputs: ProfitInputs
    template_id: UUID | None = None
