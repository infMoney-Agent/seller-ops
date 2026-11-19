import { Metadata } from 'next';
import SignInViewPage from '@/features/auth/components/sign-in-view';

export const metadata: Metadata = {
  title: '登录',
  description: '登录电商运营工作台'
};

export default async function Page() {
  return <SignInViewPage />;
}
