import PageContainer from '@/components/layout/page-container';
import {
  MembersPanel,
  type Member,
  type Invitation,
  type CompanyInfo,
  type JoinRequest
} from '@/features/auth/members-panel';
import { backendFetch, requireSession } from '@/lib/backend';

export default async function MembersPage() {
  const session = await requireSession();
  const owner = session.role === 'owner';
  const [members, invitations, company, joinRequests] = await Promise.all([
    backendFetch<Member[]>('/api/users'),
    owner ? backendFetch<Invitation[]>('/api/invitations') : [],
    backendFetch<CompanyInfo>('/api/auth/company'),
    owner ? backendFetch<JoinRequest[]>('/api/auth/join-requests') : []
  ]);
  return (
    <PageContainer pageTitle='公司成员'>
      <MembersPanel
        members={members}
        invitations={invitations}
        owner={owner}
        company={company}
        joinRequests={joinRequests}
      />
    </PageContainer>
  );
}
