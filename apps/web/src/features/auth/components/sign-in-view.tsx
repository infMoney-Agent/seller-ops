export default function SignInView() {
  return (
    <main className='mx-auto max-w-md space-y-4 p-8'>
      <h1 className='text-2xl font-semibold'>登录电商运营工作台</h1>
      <p>请使用管理员为你开通的账号。</p>
      <form action='/api/auth/login' method='get'>
        <button className='underline'>继续登录</button>
      </form>
    </main>
  );
}
