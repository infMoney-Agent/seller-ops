from pathlib import Path
from alembic import op

revision='screening_0003'
down_revision='screening_0002'
branch_labels=None
depends_on=None

def upgrade():
    sql=(Path(__file__).resolve().parents[2]/'app/screening/monthly-schema.sql').read_text()
    for statement in sql.split(';'):
        if statement.strip():op.execute(statement)

def downgrade():
    raise RuntimeError('Retain monthly history when rolling back application images')
