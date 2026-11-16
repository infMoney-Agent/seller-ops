import { Metadata } from 'next';
import SignUpViewPage from '@/features/auth/components/sign-up-view';

export const metadata: Metadata = {
  title: '注册',
  description: '注册电商运营工作台'
};

export default function Page() {
  return <SignUpViewPage />;
}
