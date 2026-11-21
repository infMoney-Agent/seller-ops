import { redirect } from 'next/navigation';
export default function SignUpView() {
  return redirect('/api/auth/login');
}
