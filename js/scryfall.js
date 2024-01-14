const fetch = require('node-fetch');
const CacheManager = require('./cache');

class Scryfall {
    constructor() {
        this.baseUrl = 'https://api.scryfall.com';
        this.endpoints = {
            sets: '/sets',
            names: '/catalog/card-names',
            types: {
                'Creature': '/catalog/creature-types',
                'Planeswalker': '/catalog/planeswalker-types',
                'Land': '/catalog/land-types',
                'Artifact': '/catalog/artifact-types',
                'Enchantment': '/catalog/enchantment-types',
                'Spells': '/catalog/spell-types'
            }
        };
    }
    async fetch(endpoint) {
        const response = await fetch(`${this.baseUrl}${endpoint}`);
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }
        try {
            const data = await response.json();
            return data.data;
        } catch (error) {
            console.error('Error parsing response:', error);
            return [];
        }
    }

    async fetchAllData() {
        const [sets, names, types] = await Promise.all([
            this.fetchSets(),
            this.fetchNames(),
            this.fetchTypes()
        ]);
        return { sets, names, types };
    }

    async getCombinedData() {
        return CacheManager.serveData(this.fetchAllData.bind(this));
    }

    async fetchTypes() {
        const typeEndpoints = this.endpoints.types;
        const typeKeys = Object.keys(typeEndpoints);
        const results = await Promise.all(
            typeKeys.map(key => this.fetch(typeEndpoints[key]))
        );

        const types = {};
        typeKeys.forEach((key, index) => {
            types[key] = results[index];
        });

        return types;
    }
    async fetchSets() {
        return this.fetch(this.endpoints.sets);
    }

    async fetchNames() {
        return this.fetch(this.endpoints.names);
    }

};

const scryfallAPI = new Scryfall();
module.exports = scryfallAPI;