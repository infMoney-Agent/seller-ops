import PageContainer from '@/components/layout/page-container';
import { MembersPanel, type Member, type Invitation } from '@/features/auth/members-panel';
import { backendFetch, requireSession } from '@/lib/backend';

export default async function MembersPage() {
  const session = await requireSession();
  if (!session.workspaceId) return <p>请选择工作区后查看成员。</p>;
  const owner = session.role === 'owner';
  const [members, invitations] = await Promise.all([
    backendFetch<Member[]>('/api/users', session.workspaceId),
    owner ? backendFetch<Invitation[]>('/api/invitations', session.workspaceId) : []
  ]);
  return (
    <PageContainer pageTitle='工作区成员'>
      <MembersPanel members={members} invitations={invitations} owner={owner} />
    </PageContainer>
  );
}
