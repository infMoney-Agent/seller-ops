import PageContainer from '@/components/layout/page-container';
import { OrgSwitcher } from '@/components/org-switcher';
import { requireSession } from '@/lib/backend';
import Link from 'next/link';

export default async function WorkspacesPage() {
  const session = await requireSession();
  return (
    <PageContainer pageTitle='工作区'>
      <div className='max-w-md space-y-4'>
        <OrgSwitcher />
        {session.workspaceId ? (
          <Link className='underline' href='/dashboard/users'>
            查看工作区成员
          </Link>
        ) : (
          <p>请联系管理员创建工作区或发送邀请。</p>
        )}
      </div>
    </PageContainer>
  );
}
