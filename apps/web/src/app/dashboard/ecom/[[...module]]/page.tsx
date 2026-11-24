import { createElement } from 'react';
import { notFound } from 'next/navigation';
import DashboardPage from '@/features/ecom/components/DashboardPage';
import ProductResearchPage from '@/features/ecom/components/ProductResearchPage';
import ListingGeneratorPage from '@/features/ecom/components/ListingGeneratorPage';
import ReviewAnalyzerPage from '@/features/ecom/components/ReviewAnalyzerPage';
import CompetitorMonitorPage from '@/features/ecom/components/CompetitorMonitorPage';
import AdOptimizerPage from '@/features/ecom/components/AdOptimizerPage';

const modules = new Map([
  ['', DashboardPage],
  ['research', ProductResearchPage],
  ['listing', ListingGeneratorPage],
  ['reviews', ReviewAnalyzerPage],
  ['monitor', CompetitorMonitorPage],
  ['ads', AdOptimizerPage]
]);

export default async function EcommercePage({
  params
}: {
  params: Promise<{ module?: string[] }>;
}) {
  const { module = [] } = await params;
  const Module = modules.get(module.join('/'));
  if (!Module) notFound();

  return (
    <div className='min-h-full min-w-0 bg-slate-950 text-slate-100'>{createElement(Module)}</div>
  );
}
