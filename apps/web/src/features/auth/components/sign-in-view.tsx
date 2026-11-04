export default function SignInView() {
  return (
    <main className='mx-auto max-w-md space-y-4 p-8'>
      <h1 className='text-2xl font-semibold'>Sign in to seller-ops</h1>
      <p>Use the account provisioned by your administrator.</p>
      <form action='/api/auth/login' method='get'>
        <button className='underline'>Continue to sign in</button>
      </form>
    </main>
  );
}
