import { LocaleDataFileSystemService, LocaleDataList } from '../../../src/common/locale-data/locale-data-filesystem.service';
import { LoggingService } from '../../../src/common/logging/logging.service';
import createMockInstance from 'jest-create-mock-instance';

describe('LocaleDataFileSystemService', () => {

    let loggingService: jest.Mocked<LoggingService>;

    beforeEach(() => {
        loggingService = createMockInstance(LoggingService);

    });

    describe('ctor', () => {
        it('should instantiate class.', () => {
            // Arrange
            const rootDir = __dirname;

            // Act
            const service = new LocaleDataFileSystemService(loggingService, rootDir);

            // Assert
            expect(service).toBeDefined();
            expect(service.rootDir).toBe(rootDir);
        });
    });

    describe('loadLocaleDataFileAsync', () => {

        it('should load Locale Data from file with valid locale data type and id.', async () => {
            // Arrange
            const rootDir = __dirname;
            const localeDataType = 'command-error';
            const localeId = 'en';
            const localeDataKey = 'CMD_01';
            const service = new LocaleDataFileSystemService(loggingService, rootDir);

            // Act
            const localeDataList: LocaleDataList = await service.loadLocaleDataFileAsync(localeDataType, localeId);

            // Assert
            expect(localeDataList).toBeDefined();
            expect(localeDataList.size).toBe(2);
            expect(localeDataList.get(localeDataKey)).toBe(localeDataList.get(localeDataKey));
            expect(service.rootDir).toBe(rootDir);
        });

        it('should throw an Error when loading Locale Data - with invalid root dir.', async () => {
            // Arrange
            const rootDir = 'INVALID';
            const localeDataType = 'command-error';
            const localeId = 'en';
            const service = new LocaleDataFileSystemService(loggingService, rootDir);

            // Act
            const promiseFct = service.loadLocaleDataFileAsync(localeDataType, localeId);

            // Assert
            await expect(promiseFct).rejects.toThrow(`ENOENT`);
        });

        it('should throw an Error when loading Locale Data - with invalid locale data type.', async () => {
            // Arrange
            const rootDir = __dirname;
            const localeDataType = 'INVALID';
            const localeId = 'en';
            const service = new LocaleDataFileSystemService(loggingService, rootDir);

            // Act
            const promiseFct = service.loadLocaleDataFileAsync(localeDataType, localeId);

            // Assert
            await expect(promiseFct).rejects.toThrow(`ENOENT`);
        });

        it('should throw an Error when loading Locale Data - with invalid local id.', async () => {
            // Arrange
            const rootDir = __dirname;
            const localeDataType = 'command-error';
            const localeId = 'invalid';
            const service = new LocaleDataFileSystemService(loggingService, rootDir);

            // Act
            const promiseFct = service.loadLocaleDataFileAsync(localeDataType, localeId);

            // Assert
            await expect(promiseFct).rejects.toThrow(`ENOENT`);
        });
    });
});