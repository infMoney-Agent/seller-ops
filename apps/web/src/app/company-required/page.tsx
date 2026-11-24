import { redirect } from 'next/navigation';
import { requireSession } from '@/lib/backend';
import CompanyRequiredActions from '@/features/auth/company-required-actions';
export default async function CompanyRequiredPage() {
  const session = await requireSession();
  if (session.workspaceId) redirect('/dashboard');
  return (
    <main className='mx-auto mt-16 max-w-md space-y-5 rounded-xl border bg-card p-6'>
      <h1 className='text-2xl font-semibold'>账号尚未加入公司</h1>
      <p className='text-sm text-muted-foreground'>当前账号：{session.email}</p>
      <p>可使用公司邀请加入，或提交加入申请，等待管理员审批。</p>
      <CompanyRequiredActions />
    </main>
  );
}
