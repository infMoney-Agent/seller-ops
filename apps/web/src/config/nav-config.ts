import type { NavGroup } from '@/types';

export const navGroups: NavGroup[] = [
  {
    label: '电商运营',
    items: [
      { title: '运营总览', url: '/dashboard/ecom', icon: 'dashboard' },
      { title: '选品研究', url: '/dashboard/ecom/research', icon: 'ecomSearch' },
      { title: '产品初筛', url: '/dashboard/ecom/screening', icon: 'product' },
      { title: '商品页面生成', url: '/dashboard/ecom/listing', icon: 'ecomFileText' },
      { title: '评论分析', url: '/dashboard/ecom/reviews', icon: 'ecomStar' },
      { title: '竞品监控', url: '/dashboard/ecom/monitor', icon: 'ecomEye' },
      { title: '广告优化', url: '/dashboard/ecom/ads', icon: 'ecomMegaphone' }
    ]
  },
  {
    label: '账号管理',
    items: [
      {
        title: '团队管理',
        url: '/dashboard/users',
        icon: 'teams',
        access: { requireOrg: true }
      },
      { title: '个人资料', url: '/dashboard/profile', icon: 'profile' }
    ]
  }
];
