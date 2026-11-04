import { requireSession } from '@/lib/backend';

export default async function ProfileViewPage() {
  const session = await requireSession();
  return (
    <div className='p-6'>
      <p>{session.name}</p>
      <p>{session.email}</p>
    </div>
  );
}
