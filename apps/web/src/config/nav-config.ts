import type { NavGroup } from '@/types';

export const navGroups: NavGroup[] = [
  {
    label: 'E-commerce',
    items: [
      { title: 'Dashboard', url: '/dashboard/ecom', icon: 'dashboard' },
      { title: 'Product Research', url: '/dashboard/ecom/research', icon: 'ecomSearch' },
      { title: 'Listing Generator', url: '/dashboard/ecom/listing', icon: 'ecomFileText' },
      { title: 'Review Analyzer', url: '/dashboard/ecom/reviews', icon: 'ecomStar' },
      { title: 'Competitor Monitor', url: '/dashboard/ecom/monitor', icon: 'ecomEye' },
      { title: 'Ad Optimizer', url: '/dashboard/ecom/ads', icon: 'ecomMegaphone' }
    ]
  },
  {
    label: 'Account',
    items: [
      { title: 'Workspaces', url: '/dashboard/workspaces', icon: 'workspace' },
      {
        title: 'Teams',
        url: '/dashboard/workspaces/team',
        icon: 'teams',
        access: { requireOrg: true }
      },
      { title: 'Profile', url: '/dashboard/profile', icon: 'profile' }
    ]
  }
];
