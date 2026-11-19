import PageContainer from '@/components/layout/page-container';
import { MembersPanel, type Member, type Invitation } from '@/features/auth/members-panel';
import { backendFetch, requireSession } from '@/lib/backend';

export default async function MembersPage() {
  const session = await requireSession();
  const owner = session.role === 'owner';
  const [members, invitations] = await Promise.all([
    backendFetch<Member[]>('/api/users'),
    owner ? backendFetch<Invitation[]>('/api/invitations') : []
  ]);
  return (
    <PageContainer pageTitle='工作区成员'>
      <MembersPanel members={members} invitations={invitations} owner={owner} />
    </PageContainer>
  );
}
