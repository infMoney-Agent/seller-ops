--
-- PostgreSQL database dump
--

\restrict 1ZNu5F3RkVoffeoFVbruajq15QMex0OyWsvVegILohTzzGk1ezwPQ9MmfUhTwVF

-- Dumped from database version 14.24 (Ubuntu 14.24-0ubuntu0.22.04.1)
-- Dumped by pg_dump version 14.24 (Ubuntu 14.24-0ubuntu0.22.04.1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'SQL_ASCII';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: ps_configs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ps_configs (
    tenant_id bigint NOT NULL,
    id uuid NOT NULL,
    version integer NOT NULL,
    name text NOT NULL,
    config jsonb NOT NULL,
    config_hash character(64) NOT NULL,
    active boolean DEFAULT false NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    kind text DEFAULT 'screening'::text NOT NULL,
    CONSTRAINT ps_configs_kind_check CHECK ((kind = ANY (ARRAY['screening'::text, 'profit_template'::text]))),
    CONSTRAINT ps_policies_version_check CHECK ((version > 0))
);


--
-- Name: ps_import_rows; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ps_import_rows (
    tenant_id bigint NOT NULL,
    id uuid NOT NULL,
    import_id uuid NOT NULL,
    member_path text DEFAULT ''::text NOT NULL,
    sheet_name text DEFAULT ''::text NOT NULL,
    row_no integer NOT NULL,
    product_id uuid,
    state text NOT NULL,
    raw_data jsonb NOT NULL,
    normalized_data jsonb,
    errors jsonb DEFAULT '[]'::jsonb NOT NULL,
    CONSTRAINT ps_import_rows_row_no_check CHECK ((row_no > 0)),
    CONSTRAINT ps_import_rows_state_check CHECK ((state = ANY (ARRAY['valid'::text, 'invalid'::text, 'committed'::text])))
);


--
-- Name: ps_imports; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ps_imports (
    tenant_id bigint NOT NULL,
    id uuid NOT NULL,
    file_name text NOT NULL,
    file_sha256 character(64) NOT NULL,
    storage_key text NOT NULL,
    file_size bigint NOT NULL,
    marketplace text DEFAULT 'US'::text NOT NULL,
    source_period text DEFAULT ''::text NOT NULL,
    observed_on date,
    mapping jsonb DEFAULT '{}'::jsonb NOT NULL,
    mapping_hash character(64) NOT NULL,
    parse_generation bigint DEFAULT 1 NOT NULL,
    state text NOT NULL,
    counts jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    committed_at timestamp with time zone,
    source_type character varying(50) DEFAULT 'legacy_import'::character varying NOT NULL,
    period_type text DEFAULT 'custom'::text NOT NULL,
    period_start date,
    period_end date,
    default_sales_scope text DEFAULT 'unknown'::text NOT NULL,
    CONSTRAINT ps_imports_check CHECK ((((period_start IS NULL) AND (period_end IS NULL)) OR ((period_start IS NOT NULL) AND (period_end IS NOT NULL) AND (period_start <= period_end)))),
    CONSTRAINT ps_imports_default_sales_scope_check CHECK ((default_sales_scope = ANY (ARRAY['child'::text, 'parent'::text, 'unknown'::text]))),
    CONSTRAINT ps_imports_file_size_check CHECK ((file_size >= 0)),
    CONSTRAINT ps_imports_marketplace_check CHECK ((marketplace = 'US'::text)),
    CONSTRAINT ps_imports_period_type_check CHECK ((period_type = ANY (ARRAY['calendar_month'::text, 'rolling_30d'::text, 'custom'::text]))),
    CONSTRAINT ps_imports_state_check CHECK ((state = ANY (ARRAY['uploaded'::text, 'parsing'::text, 'preview'::text, 'importing'::text, 'completed'::text, 'partial'::text, 'failed'::text])))
);


--
-- Name: ps_jobs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ps_jobs (
    tenant_id bigint NOT NULL,
    id uuid NOT NULL,
    kind text NOT NULL,
    state text DEFAULT 'queued'::text NOT NULL,
    idempotency_key text NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    result jsonb,
    error jsonb,
    progress integer DEFAULT 0 NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    lease_until timestamp with time zone,
    lease_token uuid,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone,
    undo_result jsonb,
    request_hash character(64),
    CONSTRAINT ps_jobs_kind_check CHECK ((kind = ANY (ARRAY['parse'::text, 'import'::text, 'screen'::text, 'profit_batch'::text, 'export'::text, 'review'::text, 'request'::text]))),
    CONSTRAINT ps_jobs_progress_check CHECK (((progress >= 0) AND (progress <= 100))),
    CONSTRAINT ps_jobs_state_check CHECK ((state = ANY (ARRAY['queued'::text, 'running'::text, 'succeeded'::text, 'partial'::text, 'failed'::text, 'preview'::text, 'applied'::text, 'undone'::text])))
);


--
-- Name: ps_parent_monthly; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ps_parent_monthly (
    tenant_id bigint NOT NULL,
    id uuid NOT NULL,
    marketplace text DEFAULT 'US'::text NOT NULL,
    parent_asin character varying(10) NOT NULL,
    stat_month date NOT NULL,
    period_type text NOT NULL,
    period_start date NOT NULL,
    period_end date NOT NULL,
    observed_on date NOT NULL,
    source_type character varying(50) NOT NULL,
    source_row_id uuid NOT NULL,
    monthly_sales bigint,
    monthly_revenue numeric(18,6),
    currency character(3) DEFAULT 'USD'::bpchar NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ps_parent_monthly_check CHECK (((period_start <= period_end) AND (stat_month = (date_trunc('month'::text, (period_end)::timestamp with time zone))::date))),
    CONSTRAINT ps_parent_monthly_check1 CHECK (((period_type <> 'rolling_30d'::text) OR ((period_end - period_start) = 29))),
    CONSTRAINT ps_parent_monthly_check2 CHECK (((period_type <> 'calendar_month'::text) OR ((period_start = stat_month) AND (period_end = ((stat_month + '1 mon -1 days'::interval))::date)))),
    CONSTRAINT ps_parent_monthly_currency_check CHECK ((currency = 'USD'::bpchar)),
    CONSTRAINT ps_parent_monthly_marketplace_check CHECK ((marketplace = 'US'::text)),
    CONSTRAINT ps_parent_monthly_monthly_revenue_check CHECK ((monthly_revenue >= (0)::numeric)),
    CONSTRAINT ps_parent_monthly_monthly_sales_check CHECK ((monthly_sales >= 0)),
    CONSTRAINT ps_parent_monthly_parent_asin_check CHECK (((parent_asin)::text ~ '^[A-Z0-9]{10}$'::text)),
    CONSTRAINT ps_parent_monthly_period_type_check CHECK ((period_type = ANY (ARRAY['calendar_month'::text, 'rolling_30d'::text, 'custom'::text]))),
    CONSTRAINT ps_parent_monthly_stat_month_check CHECK ((EXTRACT(day FROM stat_month) = (1)::numeric))
);


--
-- Name: ps_product_monthly; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ps_product_monthly (
    tenant_id bigint NOT NULL,
    id uuid NOT NULL,
    product_id uuid NOT NULL,
    stat_month date NOT NULL,
    period_type text NOT NULL,
    period_start date NOT NULL,
    period_end date NOT NULL,
    observed_on date NOT NULL,
    source_type character varying(50) NOT NULL,
    source_row_id uuid NOT NULL,
    currency character(3) DEFAULT 'USD'::bpchar NOT NULL,
    price_usd numeric(18,6),
    monthly_sales bigint,
    monthly_revenue numeric(18,6),
    review_count bigint,
    rating numeric(3,2),
    bsr_rank bigint,
    revision bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    sales_scope text DEFAULT 'unknown'::text NOT NULL,
    parent_asin_snapshot character varying(10),
    CONSTRAINT ps_product_monthly_bsr_rank_check CHECK ((bsr_rank >= 0)),
    CONSTRAINT ps_product_monthly_check CHECK (((period_start <= period_end) AND (stat_month = (date_trunc('month'::text, (period_end)::timestamp with time zone))::date))),
    CONSTRAINT ps_product_monthly_check1 CHECK (((period_type <> 'rolling_30d'::text) OR ((period_end - period_start) = 29))),
    CONSTRAINT ps_product_monthly_check2 CHECK (((period_type <> 'calendar_month'::text) OR ((period_start = stat_month) AND (period_end = ((stat_month + '1 mon -1 days'::interval))::date)))),
    CONSTRAINT ps_product_monthly_currency_check CHECK ((currency = 'USD'::bpchar)),
    CONSTRAINT ps_product_monthly_monthly_revenue_check CHECK ((monthly_revenue >= (0)::numeric)),
    CONSTRAINT ps_product_monthly_monthly_sales_check CHECK ((monthly_sales >= 0)),
    CONSTRAINT ps_product_monthly_parent_asin_snapshot_check CHECK (((parent_asin_snapshot IS NULL) OR ((parent_asin_snapshot)::text ~ '^[A-Z0-9]{10}$'::text))),
    CONSTRAINT ps_product_monthly_period_type_check CHECK ((period_type = ANY (ARRAY['calendar_month'::text, 'rolling_30d'::text, 'custom'::text]))),
    CONSTRAINT ps_product_monthly_price_usd_check CHECK ((price_usd >= (0)::numeric)),
    CONSTRAINT ps_product_monthly_rating_check CHECK (((rating >= (0)::numeric) AND (rating <= (5)::numeric))),
    CONSTRAINT ps_product_monthly_review_count_check CHECK ((review_count >= 0)),
    CONSTRAINT ps_product_monthly_revision_check CHECK ((revision > 0)),
    CONSTRAINT ps_product_monthly_sales_scope_check CHECK ((sales_scope = ANY (ARRAY['child'::text, 'parent'::text, 'unknown'::text]))),
    CONSTRAINT ps_product_monthly_stat_month_check CHECK ((EXTRACT(day FROM stat_month) = (1)::numeric))
);


--
-- Name: ps_products; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ps_products (
    tenant_id bigint NOT NULL,
    id uuid NOT NULL,
    marketplace text DEFAULT 'US'::text NOT NULL,
    asin character varying(10) NOT NULL,
    title text NOT NULL,
    title_zh text,
    image_url text,
    category text,
    brand text,
    seller_id text,
    seller_name text,
    seller_region text,
    specs jsonb DEFAULT '{}'::jsonb NOT NULL,
    current_row_id uuid,
    review_status text DEFAULT 'pending'::text NOT NULL,
    review_reason text DEFAULT ''::text NOT NULL,
    review_note text DEFAULT ''::text NOT NULL,
    reviewed_by bigint,
    reviewed_at timestamp with time zone,
    data_revision bigint DEFAULT 0 NOT NULL,
    review_revision bigint DEFAULT 0 NOT NULL,
    screen_generation bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    current_group_rep_id uuid,
    current_monthly_id uuid,
    current_monthly_revision bigint,
    parent_asin character varying(10),
    manual_category text DEFAULT ''::text NOT NULL,
    material text,
    weight_g numeric(18,6),
    package_weight_g numeric(18,6),
    package_length_cm numeric(18,6),
    package_width_cm numeric(18,6),
    package_height_cm numeric(18,6),
    units_per_pack integer,
    carton_units integer,
    carton_weight_g numeric(18,6),
    carton_length_cm numeric(18,6),
    carton_width_cm numeric(18,6),
    carton_height_cm numeric(18,6),
    machine_decision text,
    machine_recommendation text,
    machine_direction text,
    machine_group_key text,
    machine_reasons jsonb DEFAULT '[]'::jsonb NOT NULL,
    machine_evidence jsonb DEFAULT '[]'::jsonb NOT NULL,
    machine_policy_id uuid,
    machine_source_row_id uuid,
    machine_monthly_id uuid,
    machine_monthly_revision bigint,
    screened_at timestamp with time zone,
    CONSTRAINT ps_products_asin_check CHECK (((asin)::text ~ '^[A-Z0-9]{10}$'::text)),
    CONSTRAINT ps_products_carton_height_cm_check CHECK ((carton_height_cm >= (0)::numeric)),
    CONSTRAINT ps_products_carton_length_cm_check CHECK ((carton_length_cm >= (0)::numeric)),
    CONSTRAINT ps_products_carton_units_check CHECK ((carton_units > 0)),
    CONSTRAINT ps_products_carton_weight_g_check CHECK ((carton_weight_g >= (0)::numeric)),
    CONSTRAINT ps_products_carton_width_cm_check CHECK ((carton_width_cm >= (0)::numeric)),
    CONSTRAINT ps_products_check CHECK (((review_status <> ALL (ARRAY['excluded'::text, 'unsure'::text])) OR (length(TRIM(BOTH FROM review_reason)) > 0))),
    CONSTRAINT ps_products_parent_asin_format CHECK (((parent_asin IS NULL) OR ((parent_asin)::text ~ '^[A-Z0-9]{10}$'::text))),
    CONSTRAINT ps_products_machine_decision_check CHECK ((machine_decision = ANY (ARRAY['passed'::text, 'excluded'::text, 'insufficient'::text]))),
    CONSTRAINT ps_products_machine_recommendation_check CHECK ((machine_recommendation = ANY (ARRAY['regular'::text, 'exploration'::text]))),
    CONSTRAINT ps_products_marketplace_check CHECK ((marketplace = 'US'::text)),
    CONSTRAINT ps_products_package_height_cm_check CHECK ((package_height_cm >= (0)::numeric)),
    CONSTRAINT ps_products_package_length_cm_check CHECK ((package_length_cm >= (0)::numeric)),
    CONSTRAINT ps_products_package_weight_g_check CHECK ((package_weight_g >= (0)::numeric)),
    CONSTRAINT ps_products_package_width_cm_check CHECK ((package_width_cm >= (0)::numeric)),
    CONSTRAINT ps_products_review_status_check CHECK ((review_status = ANY (ARRAY['pending'::text, 'kept'::text, 'excluded'::text, 'unsure'::text]))),
    CONSTRAINT ps_products_units_per_pack_check CHECK ((units_per_pack > 0)),
    CONSTRAINT ps_products_weight_g_check CHECK ((weight_g >= (0)::numeric))
);


--
-- Name: ps_profit_scenarios; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ps_profit_scenarios (
    tenant_id bigint NOT NULL,
    id uuid NOT NULL,
    product_id uuid NOT NULL,
    template_id uuid,
    name text NOT NULL,
    inputs jsonb NOT NULL,
    output jsonb,
    template_snapshot jsonb,
    formula_version text NOT NULL,
    product_data_revision bigint NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    created_by bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    monthly_id uuid,
    monthly_revision bigint,
    monthly_snapshot jsonb,
    product_snapshot jsonb
);


--
-- Name: ps_review_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ps_review_events (
    tenant_id bigint NOT NULL,
    id uuid NOT NULL,
    product_id uuid NOT NULL,
    batch_id uuid,
    undo_of uuid,
    actor_id bigint NOT NULL,
    before_revision bigint NOT NULL,
    after_revision bigint NOT NULL,
    before_value jsonb NOT NULL,
    after_value jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: ps_configs ps_configs_tenant_id_kind_name_version_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_configs
    ADD CONSTRAINT ps_configs_tenant_id_kind_name_version_key UNIQUE (tenant_id, kind, name, version);


--
-- Name: ps_import_rows ps_import_rows_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_import_rows
    ADD CONSTRAINT ps_import_rows_pkey PRIMARY KEY (tenant_id, id);


--
-- Name: ps_import_rows ps_import_rows_tenant_id_import_id_member_path_sheet_name_r_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_import_rows
    ADD CONSTRAINT ps_import_rows_tenant_id_import_id_member_path_sheet_name_r_key UNIQUE (tenant_id, import_id, member_path, sheet_name, row_no);


--
-- Name: ps_imports ps_imports_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_imports
    ADD CONSTRAINT ps_imports_pkey PRIMARY KEY (tenant_id, id);


--
-- Name: ps_jobs ps_jobs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_jobs
    ADD CONSTRAINT ps_jobs_pkey PRIMARY KEY (tenant_id, id);


--
-- Name: ps_jobs ps_jobs_tenant_id_kind_idempotency_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_jobs
    ADD CONSTRAINT ps_jobs_tenant_id_kind_idempotency_key_key UNIQUE (tenant_id, kind, idempotency_key);


--
-- Name: ps_parent_monthly ps_parent_monthly_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_parent_monthly
    ADD CONSTRAINT ps_parent_monthly_pkey PRIMARY KEY (tenant_id, id);


--
-- Name: ps_parent_monthly ps_parent_monthly_tenant_id_marketplace_parent_asin_source__key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_parent_monthly
    ADD CONSTRAINT ps_parent_monthly_tenant_id_marketplace_parent_asin_source__key UNIQUE (tenant_id, marketplace, parent_asin, source_type, period_type, period_start, period_end);


--
-- Name: ps_configs ps_policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_configs
    ADD CONSTRAINT ps_policies_pkey PRIMARY KEY (tenant_id, id);


--
-- Name: ps_product_monthly ps_product_monthly_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_product_monthly
    ADD CONSTRAINT ps_product_monthly_pkey PRIMARY KEY (tenant_id, id);


--
-- Name: ps_product_monthly ps_product_monthly_tenant_id_product_id_source_type_period__key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_product_monthly
    ADD CONSTRAINT ps_product_monthly_tenant_id_product_id_source_type_period__key UNIQUE (tenant_id, product_id, source_type, period_type, period_start, period_end);


--
-- Name: ps_products ps_products_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_products
    ADD CONSTRAINT ps_products_pkey PRIMARY KEY (tenant_id, id);


--
-- Name: ps_products ps_products_tenant_id_marketplace_asin_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_products
    ADD CONSTRAINT ps_products_tenant_id_marketplace_asin_key UNIQUE (tenant_id, marketplace, asin);


--
-- Name: ps_profit_scenarios ps_profit_scenarios_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_profit_scenarios
    ADD CONSTRAINT ps_profit_scenarios_pkey PRIMARY KEY (tenant_id, id);


--
-- Name: ps_review_events ps_review_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_review_events
    ADD CONSTRAINT ps_review_events_pkey PRIMARY KEY (tenant_id, id);


--
-- Name: ps_review_events ps_review_events_tenant_id_product_id_after_revision_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_review_events
    ADD CONSTRAINT ps_review_events_tenant_id_product_id_after_revision_key UNIQUE (tenant_id, product_id, after_revision);


--
-- Name: ps_review_events ps_review_events_tenant_id_undo_of_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_review_events
    ADD CONSTRAINT ps_review_events_tenant_id_undo_of_key UNIQUE (tenant_id, undo_of);


--
-- Name: ps_import_identity; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX ps_import_identity ON public.ps_imports USING btree (tenant_id, file_sha256, marketplace, source_period, mapping_hash, COALESCE(observed_on, '0001-01-01'::date), source_type, period_type, COALESCE(period_start, '0001-01-01'::date), COALESCE(period_end, '0001-01-01'::date), default_sales_scope);


--
-- Name: ps_import_row_product_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_import_row_product_idx ON public.ps_import_rows USING btree (tenant_id, product_id, import_id);


--
-- Name: ps_job_queue_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_job_queue_idx ON public.ps_jobs USING btree (state, lease_until, created_at);


--
-- Name: ps_job_review_history_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_job_review_history_idx ON public.ps_jobs USING btree (tenant_id, created_at DESC) WHERE (kind = 'review'::text);


--
-- Name: ps_monthly_month_sales_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_monthly_month_sales_idx ON public.ps_product_monthly USING btree (tenant_id, stat_month, monthly_sales DESC);


--
-- Name: ps_monthly_period_lookup_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_monthly_period_lookup_idx ON public.ps_product_monthly USING btree (tenant_id, stat_month, period_type, product_id, period_end DESC, observed_on DESC, updated_at DESC, id DESC) INCLUDE (price_usd, monthly_sales);


--
-- Name: ps_monthly_product_month_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_monthly_product_month_idx ON public.ps_product_monthly USING btree (tenant_id, product_id, stat_month DESC, period_end DESC, observed_on DESC);


--
-- Name: ps_monthly_source_row_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_monthly_source_row_idx ON public.ps_product_monthly USING btree (tenant_id, source_row_id);


--
-- Name: ps_one_active_config; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX ps_one_active_config ON public.ps_configs USING btree (tenant_id) WHERE (active AND (kind = 'screening'::text));


--
-- Name: ps_parent_month_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_parent_month_idx ON public.ps_parent_monthly USING btree (tenant_id, marketplace, parent_asin, stat_month DESC, period_end DESC);


--
-- Name: ps_product_family_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_product_family_idx ON public.ps_products USING btree (tenant_id, marketplace, parent_asin);


--
-- Name: ps_product_machine_group_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_product_machine_group_idx ON public.ps_products USING btree (tenant_id, machine_group_key);


--
-- Name: ps_product_machine_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_product_machine_idx ON public.ps_products USING btree (tenant_id, machine_decision, machine_recommendation);


--
-- Name: ps_product_reference_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_product_reference_idx ON public.ps_products USING btree (tenant_id, machine_direction, id) WHERE (review_status <> 'pending'::text);


--
-- Name: ps_product_review_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_product_review_idx ON public.ps_products USING btree (tenant_id, review_status, updated_at DESC, id);


--
-- Name: ps_product_seller_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_product_seller_idx ON public.ps_products USING btree (tenant_id, seller_region, seller_id);


--
-- Name: ps_profit_monthly_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_profit_monthly_idx ON public.ps_profit_scenarios USING btree (tenant_id, product_id, monthly_id, created_at DESC, id DESC);


--
-- Name: ps_profit_product_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_profit_product_idx ON public.ps_profit_scenarios USING btree (tenant_id, product_id, created_at DESC);


--
-- Name: ps_review_history_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ps_review_history_idx ON public.ps_review_events USING btree (tenant_id, product_id, created_at DESC);


--
-- Name: ps_import_rows ps_import_rows_tenant_id_import_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_import_rows
    ADD CONSTRAINT ps_import_rows_tenant_id_import_id_fkey FOREIGN KEY (tenant_id, import_id) REFERENCES public.ps_imports(tenant_id, id);


--
-- Name: ps_import_rows ps_import_rows_tenant_id_product_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_import_rows
    ADD CONSTRAINT ps_import_rows_tenant_id_product_id_fkey FOREIGN KEY (tenant_id, product_id) REFERENCES public.ps_products(tenant_id, id);


--
-- Name: ps_parent_monthly ps_parent_monthly_tenant_id_source_row_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_parent_monthly
    ADD CONSTRAINT ps_parent_monthly_tenant_id_source_row_id_fkey FOREIGN KEY (tenant_id, source_row_id) REFERENCES public.ps_import_rows(tenant_id, id);


--
-- Name: ps_product_monthly ps_product_monthly_tenant_id_product_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_product_monthly
    ADD CONSTRAINT ps_product_monthly_tenant_id_product_id_fkey FOREIGN KEY (tenant_id, product_id) REFERENCES public.ps_products(tenant_id, id);


--
-- Name: ps_product_monthly ps_product_monthly_tenant_id_source_row_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_product_monthly
    ADD CONSTRAINT ps_product_monthly_tenant_id_source_row_id_fkey FOREIGN KEY (tenant_id, source_row_id) REFERENCES public.ps_import_rows(tenant_id, id);


--
-- Name: ps_products ps_products_tenant_id_current_group_rep_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_products
    ADD CONSTRAINT ps_products_tenant_id_current_group_rep_id_fkey FOREIGN KEY (tenant_id, current_group_rep_id) REFERENCES public.ps_products(tenant_id, id);


--
-- Name: ps_products ps_products_tenant_id_current_monthly_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_products
    ADD CONSTRAINT ps_products_tenant_id_current_monthly_id_fkey FOREIGN KEY (tenant_id, current_monthly_id) REFERENCES public.ps_product_monthly(tenant_id, id);


--
-- Name: ps_products ps_products_tenant_id_current_row_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_products
    ADD CONSTRAINT ps_products_tenant_id_current_row_id_fkey FOREIGN KEY (tenant_id, current_row_id) REFERENCES public.ps_import_rows(tenant_id, id);


--
-- Name: ps_products ps_products_tenant_id_machine_monthly_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_products
    ADD CONSTRAINT ps_products_tenant_id_machine_monthly_id_fkey FOREIGN KEY (tenant_id, machine_monthly_id) REFERENCES public.ps_product_monthly(tenant_id, id);


--
-- Name: ps_products ps_products_tenant_id_machine_policy_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_products
    ADD CONSTRAINT ps_products_tenant_id_machine_policy_id_fkey FOREIGN KEY (tenant_id, machine_policy_id) REFERENCES public.ps_configs(tenant_id, id);


--
-- Name: ps_products ps_products_tenant_id_machine_source_row_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_products
    ADD CONSTRAINT ps_products_tenant_id_machine_source_row_id_fkey FOREIGN KEY (tenant_id, machine_source_row_id) REFERENCES public.ps_import_rows(tenant_id, id);


--
-- Name: ps_profit_scenarios ps_profit_scenarios_tenant_id_monthly_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_profit_scenarios
    ADD CONSTRAINT ps_profit_scenarios_tenant_id_monthly_id_fkey FOREIGN KEY (tenant_id, monthly_id) REFERENCES public.ps_product_monthly(tenant_id, id);


--
-- Name: ps_profit_scenarios ps_profit_scenarios_tenant_id_product_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_profit_scenarios
    ADD CONSTRAINT ps_profit_scenarios_tenant_id_product_id_fkey FOREIGN KEY (tenant_id, product_id) REFERENCES public.ps_products(tenant_id, id);


--
-- Name: ps_profit_scenarios ps_profit_scenarios_tenant_id_template_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_profit_scenarios
    ADD CONSTRAINT ps_profit_scenarios_tenant_id_template_id_fkey FOREIGN KEY (tenant_id, template_id) REFERENCES public.ps_configs(tenant_id, id);


--
-- Name: ps_review_events ps_review_events_tenant_id_batch_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_review_events
    ADD CONSTRAINT ps_review_events_tenant_id_batch_id_fkey FOREIGN KEY (tenant_id, batch_id) REFERENCES public.ps_jobs(tenant_id, id);


--
-- Name: ps_review_events ps_review_events_tenant_id_product_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_review_events
    ADD CONSTRAINT ps_review_events_tenant_id_product_id_fkey FOREIGN KEY (tenant_id, product_id) REFERENCES public.ps_products(tenant_id, id);


--
-- Name: ps_review_events ps_review_events_tenant_id_undo_of_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ps_review_events
    ADD CONSTRAINT ps_review_events_tenant_id_undo_of_fkey FOREIGN KEY (tenant_id, undo_of) REFERENCES public.ps_review_events(tenant_id, id);


--
-- PostgreSQL database dump complete
--

\unrestrict 1ZNu5F3RkVoffeoFVbruajq15QMex0OyWsvVegILohTzzGk1ezwPQ9MmfUhTwVF
