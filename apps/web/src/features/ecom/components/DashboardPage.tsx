import { Icons } from '@/components/icons';
import Link from 'next/link';
import { cn } from '@/lib/utils';

const {
  ecomSearch: Search,
  ecomTrendingUp: TrendingUp,
  ecomFileText: FileText,
  ecomStar: Star,
  ecomEye: Eye,
  ecomMegaphone: Megaphone,
  ecomArrowRight: ArrowRight,
  ecomZap: Zap
} = Icons;

const cards = [
  {
    to: '/dashboard/ecom/research',
    icon: Search,
    title: '选品研究',
    description: '分析亚马逊各类目的畅销商品并给出评分。',
    color: 'from-blue-500/20 to-blue-600/10 border-blue-500/20',
    iconColor: 'text-blue-400',
    stat: '商品评分 0–10 分'
  },
  {
    to: '/dashboard/ecom/listing',
    icon: FileText,
    title: '商品页面生成',
    description: '一键生成多语言亚马逊商品页面文案。',
    color: 'from-purple-500/20 to-purple-600/10 border-purple-500/20',
    iconColor: 'text-purple-400',
    stat: '支持 6 种语言'
  },
  {
    to: '/dashboard/ecom/reviews',
    icon: Star,
    title: '评论分析',
    description: '从评论中提炼用户痛点、好评亮点和改进建议。',
    color: 'from-amber-500/20 to-amber-600/10 border-amber-500/20',
    iconColor: 'text-amber-400',
    stat: 'AI 评论分析'
  },
  {
    to: '/dashboard/ecom/monitor',
    icon: Eye,
    title: '竞品监控',
    description: '追踪指定 ASIN 的价格、畅销排名和评论变化，并查看异常提醒。',
    color: 'from-emerald-500/20 to-emerald-600/10 border-emerald-500/20',
    iconColor: 'text-emerald-400',
    stat: '每小时自动记录'
  },
  {
    to: '/dashboard/ecom/ads',
    icon: Megaphone,
    title: '广告优化',
    description: '分析按点击付费的广告活动，获取关键词出价建议。',
    color: 'from-rose-500/20 to-rose-600/10 border-rose-500/20',
    iconColor: 'text-rose-400',
    stat: '亚马逊广告 API'
  }
];

export default function DashboardPage() {
  return (
    <div className='p-4 sm:p-8 max-w-7xl mx-auto'>
      {/* Header */}
      <div className='mb-10'>
        <div className='flex items-center gap-3 mb-3'>
          <div className='w-10 h-10 rounded-xl bg-blue-600 flex items-center justify-center'>
            <Zap className='w-6 h-6 text-white' />
          </div>
          <h1 className='text-3xl font-bold text-white'>电商运营工作台</h1>
        </div>
        <p className='text-slate-400 text-lg max-w-2xl'>
          使用 AI 辅助亚马逊选品、商品页面编写、评论分析、竞品监控与广告优化。
        </p>
      </div>

      {/* Workflow hint */}
      <div className='mb-8 p-4 rounded-xl bg-blue-950/50 border border-blue-800/40 flex items-center gap-3 flex-wrap'>
        <TrendingUp className='w-5 h-5 text-blue-400 flex-shrink-0' />
        <span className='text-sm text-blue-200 font-medium'>推荐流程</span>
        {['选品研究', '评论分析', '商品页面生成', '竞品监控', '广告优化'].map((step, i, arr) => (
          <span key={step} className='flex items-center gap-2'>
            <span className='text-sm text-blue-300'>{step}</span>
            {i < arr.length - 1 && <ArrowRight className='w-4 h-4 text-blue-600' />}
          </span>
        ))}
      </div>

      {/* Module cards */}
      <div className='grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-5'>
        {cards.map(({ to, icon: Icon, title, description, color, iconColor, stat }) => (
          <Link
            key={to}
            href={to}
            className={cn(
              'group relative rounded-2xl border bg-gradient-to-br p-6 transition-all hover:scale-[1.02] hover:shadow-xl hover:shadow-black/30',
              color
            )}
          >
            <div
              className={cn(
                'w-12 h-12 rounded-xl bg-slate-900/60 flex items-center justify-center mb-4',
                iconColor
              )}
            >
              <Icon className='w-6 h-6' />
            </div>
            <h2 className='text-white font-semibold text-lg mb-2'>{title}</h2>
            <p className='text-slate-400 text-sm leading-relaxed mb-4'>{description}</p>
            <div className='flex items-center justify-between'>
              <span className='text-xs text-slate-500 font-mono bg-slate-900/40 px-2 py-1 rounded'>
                {stat}
              </span>
              <ArrowRight className='w-4 h-4 text-slate-500 group-hover:text-white group-hover:translate-x-1 transition-all' />
            </div>
          </Link>
        ))}
      </div>

      {/* Open source callout */}
      <div className='mt-10 p-5 rounded-xl bg-slate-900 border border-slate-800 flex items-center justify-between flex-wrap gap-4'>
        <div>
          <p className='text-white font-semibold'>开源与自托管</p>
          <p className='text-slate-400 text-sm mt-1'>
            可连接自己的 OpenAI、Claude、Gemini 或 Ollama。
          </p>
        </div>
        <a
          href='https://github.com/yoligehude14753/ecom-agent'
          target='_blank'
          rel='noopener noreferrer'
          className='flex items-center gap-2 px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700 text-white text-sm font-medium transition-colors'
        >
          在 GitHub 查看
          <ArrowRight className='w-4 h-4' />
        </a>
      </div>
    </div>
  );
}
