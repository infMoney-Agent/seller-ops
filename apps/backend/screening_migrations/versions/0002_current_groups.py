from alembic import op

revision = "screening_0002"
down_revision = "screening_0001"
branch_labels = None
depends_on = None


def upgrade():
    op.execute("ALTER TABLE ps_products ADD COLUMN current_group_rep_id uuid")
    op.execute(
        "ALTER TABLE ps_products ADD FOREIGN KEY(tenant_id,current_group_rep_id) REFERENCES ps_products(tenant_id,id)"
    )


def downgrade():
    raise RuntimeError("Retain screening data when rolling back application images")
