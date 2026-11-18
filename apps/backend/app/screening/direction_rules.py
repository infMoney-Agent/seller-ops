import re


def direction(r):
    t = r[2].lower()
    c = str(r[3])
    if re.search("cheese\\s?cloth", t):
        return "食品过滤纱布"
    if re.search("vacuum|compression", t):
        return "床垫真空压缩袋" if "mattress" in t else "手动泵衣物真空压缩袋"
    if c in ["Cups", "Disposable Cups", "Disposable Shot Glasses"] or "Party Cups" in c:
        if re.search("jello|portion|souffl", t):
            return "带盖食品分装杯"
        if "dessert" in t:
            return "带盖甜品杯"
        if "shot" in t:
            return "一次性塑料小酒杯"
        if "straws" in t and "strawless" not in t:
            return "带盖吸管塑料饮料杯"
        if "lid" in t:
            return "带盖免吸管塑料饮料杯"
        return "无盖塑料饮料杯"
    if c == "Disposable Plates":
        return (
            "一次性塑料餐盘"
            if "plastic" in t
            else "一次性纸浆餐盘"
            if re.search("compostable|bagasse|sugarcane", t)
            else "一次性普通纸餐盘"
        )
    if c == "Disposable Bowls":
        return ("带盖" if "lid" in t else "无盖") + (
            "竹纤维餐碗" if "bamboo" in t else "纸餐碗"
        )
    if re.search("silverware|forks and spoons", t):
        return "一次性塑料餐具组合"
    if re.search("plastic.*fork|fork.*plastic", t):
        return "一次性塑料叉"
    if re.search("plastic.*spoon|spoon.*plastic", t):
        return "一次性塑料勺"
    if re.search("plastic.*kniv|kniv.*plastic", t):
        return "一次性塑料刀"
    if c == "Trash Bags":
        return (
            "建筑重型垃圾袋"
            if re.search("contractor|construction|industrial", t)
            else "家用垃圾袋"
        )
    if c == "Disposable Compost Bags":
        return "可堆肥垃圾袋"
    if c == "Household Cleaning Sponges":
        if "melamine" in t:
            return "三聚氰胺清洁海绵"
        if re.search("damp.*duster|dusting sponge", t):
            return "除尘清洁海绵"
        if "long handle" in t:
            return "长柄浴室刷配替换擦片"
        if re.search("coconut", t):
            return "椰纤维复合洗碗海绵"
        if re.search("cellulose|natural dish", t):
            return "植物纤维洗碗海绵"
        return "复合材质洗碗海绵"
    if re.search(
        "mop.*(replacement|refill|replace)|replacement mop|mop head|sweeping cloth|power mop pads|powermop.*pads",
        t,
    ):
        if "narwal" in t:
            return "Narwal适配拖布"
        if "vacmop" in t:
            return "Shark VACMOP适配拖布"
        if "powermop" in t or "power mop" in t:
            return "Swiffer PowerMop适配拖布"
        if "sweeper" in t:
            return (
                "Swiffer Sweeper可洗拖布"
                if re.search("washable|reusable", t)
                else "Swiffer Sweeper一次性除尘布"
            )
        if "rinseclean" in t or "2-tank" in t:
            return "O-Cedar双桶适配拖把头"
        if re.search("o.?cedar|ocedar", t):
            return "O-Cedar单桶适配拖把头"
        if "xxflower" in t:
            return "XXFLOWER适配拧水拖把头"
        if "looped" in t:
            return "环形线商用拖把头"
        return "通用纤维拖把替换头"
    if c in ["Reuseable Cleaning Cloths", "Dish Cloths & Dish Towels"]:
        if "swedish" in t:
            return "瑞典洗碗布"
        if "disposable" in t:
            return "一次性清洁布"
        if "cotton" in t:
            return "棉质洗碗布"
        if "twisted" in t:
            return "绞圈吸水清洁布"
        if "roll" in t:
            return "卷式超细纤维清洁布"
        return "超细纤维清洁布"
    if c in ["Aluminum Foil Wrap", "Aluminum Foil"]:
        return "预切铝箔片" if re.search("sheet|pre.cut", t) else "铝箔卷"
    if c == "Disposable Food Storage Containers":
        return "一次性带盖铝箔餐盒"
    if c == "Disposable Food Storage Bags":
        if "slow cooker" in t:
            return "慢炖锅内衬袋"
        if "hot dog" in t:
            return "铝箔热狗包装袋"
        if "paper" in t:
            return "牛皮纸食品袋"
        return "塑料食品密封储存袋"
    if c == "Makeup Blenders & Sponges":
        return "压缩纤维洁面海绵" if "compressed" in t else "楔形化妆海绵"
    if c == "Feather Dusters":
        return "一次性除尘掸替换头"
    if c == "Household Cleaning Gloves":
        return "丁腈一次性手套" if "nitrile" in t else "塑料食品操作手套"
    return c.replace("\r", "").replace("\n", " / ")
