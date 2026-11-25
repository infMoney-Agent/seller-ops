import type { Metadata } from 'next';
import { redirect } from 'next/navigation';
export const metadata: Metadata = {
  title: '登录 · Seller Ops',
  description: '登录Seller Ops电商运营工作台'
};
export default async function Page({
  searchParams
}: {
  searchParams: Promise<{ error?: string }>;
}) {
  const params = await searchParams;
  if (params.error)
    return (
      <main className='mx-auto max-w-md space-y-4 p-8'>
        <h1 className='text-2xl font-semibold'>登录暂未完成</h1>
        <p>请重新登录 Seller Ops 电商运营工作台。</p>
        <form action='/api/auth/login' method='get'>
          <button type='submit' className='underline'>
            重新登录
          </button>
        </form>
      </main>
    );
  redirect('/api/auth/login');
}
