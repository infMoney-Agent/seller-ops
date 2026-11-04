import PageContainer from '@/components/layout/page-container';
import { OrgSwitcher } from '@/components/org-switcher';
import { requireSession } from '@/lib/backend';
import Link from 'next/link';

export default async function WorkspacesPage() {
  const session = await requireSession();
  return (
    <PageContainer pageTitle='Workspaces'>
      <div className='max-w-md space-y-4'>
        <OrgSwitcher />
        {session.workspaceId ? (
          <Link className='underline' href='/dashboard/users'>
            View workspace members
          </Link>
        ) : (
          <p>Ask your administrator to create a workspace or share an invitation.</p>
        )}
      </div>
    </PageContainer>
  );
}
