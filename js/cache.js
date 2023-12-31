const fs = require('fs');

const CacheManager = {
    cache: {
        data: { lastFetch: 0, content: {} },
    },
    async serveData(fetchFunc, maxAge = 24 * 60 * 60 * 1000) {
        const cache = this.cache.data;
        const now = new Date().getTime();
        const isCacheExpired = now - cache.lastFetch > maxAge;
    
        if (isCacheExpired || !cache.content || Object.keys(cache.content).length === 0) {
            console.log('Cache is expired or empty. Fetching new data.');
            cache.content = await fetchFunc();
            cache.lastFetch = now;
            this.saveCacheToFile();
        } else {
            console.log('Serving cached data.');
        }
        return cache.content;
    },

    saveCacheToFile() {
        const cacheFilePath = './cache.json';
        fs.writeFileSync(cacheFilePath, JSON.stringify(this.cache, null, 2), 'utf-8');
    },

    loadCacheFromFile() {
        const cacheFilePath = './cache.json';
        if (fs.existsSync(cacheFilePath)) {
            try {
                const fileContent = fs.readFileSync(cacheFilePath, 'utf-8');
                this.cache.data = JSON.parse(fileContent);
            } catch (error) {
                console.error('Error loading cache:', error);
            }
        }
    },

    init() {
        this.loadCacheFromFile();
    }
};

CacheManager.init();

module.exports = CacheManager;
