
import { JsonDataService } from '../../../src/common/data-service/json.data-service';
import { IIdentifiable } from '../../../src/common/data-service/identifiable';

export interface Salvo extends IIdentifiable {
    name: string;
    description: string;
}
export class SalvoDataService extends JsonDataService<Salvo> {

    private static readonly SALVO_FILE_PATH = `${__dirname}/salvo-data.json`;

    constructor() {
        super(SalvoDataService.SALVO_FILE_PATH);
    }
}

describe('SalvoDataService', () => {

    it('Should test the data service', async () => {
        // Create a service
        const service = new SalvoDataService();

        // Load the Json file
        console.log('loadAsync...');
        await service.loadAsync();

        // Create a Salvo, add it to the list and save it to json (autosave)
        const salvo: Salvo = {
            id: '123456789',
            name: 'name-01',
            description: 'description-01'
        };
        await service.addAsync(salvo, true);
        console.log('addAsync...');

        // Get all salvo
        let allData = service.getAll();
        console.log('getAll...\n', JSON.stringify(allData, null, 2));

        // Get Salvo by id
        const data = await service.getById(salvo.id);
        console.log('getById...\n', JSON.stringify(data, null, 2));

        // Delete Salvo (bo auto save)
        await service.deleteAsync(salvo);
        console.log('deleteAsync...');

        allData = service.getAll();
        console.log('getAll...\n', JSON.stringify(allData, null, 2));


        // Save to json
        await service.saveAsync();
        console.log('saveAsync...');
    });
});
