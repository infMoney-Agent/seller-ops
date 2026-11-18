from pathlib import Path
from alembic import op

revision = "screening_0001"
down_revision = None
branch_labels = None
depends_on = None


def upgrade():
    sql = (Path(__file__).resolve().parents[2] / "app/screening/schema.sql").read_text()
    for statement in sql.split(";"):
        if statement.strip():
            op.execute(statement)


def downgrade():
    raise RuntimeError(
        "Destructive downgrade disabled: restore application image, retain screening data"
    )
