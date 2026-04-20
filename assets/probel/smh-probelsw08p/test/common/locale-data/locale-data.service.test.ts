import { LocaleDataervice } from '../../../src/common/locale-data/locale-data.service';
import createMockInstance from 'jest-create-mock-instance';
import { LoggingService, LogMessage } from '../../../src/common/logging/logging.service';
import { LocaleDataFileSystemService } from '../../../src/common/locale-data/locale-data-filesystem.service';
import { LocaleDataError } from '../../../src/common/locale-data/locale-data.error';
import { LocaleData } from '../../../src/common/locale-data/locale-data.model';
import { doesNotMatch } from 'assert';

describe('LocalDataervice', () => {

    let loggingService: jest.Mocked<LoggingService>;

    beforeEach(() => {
        loggingService = createMockInstance(LoggingService);
    });

    describe('ctor', () => {
        it('should instantiate class.', () => {
            // Arrange
            const rootDir = __dirname;
            let localeDataFileSystemService = new LocaleDataFileSystemService(loggingService, rootDir);

            // Act
            const service = new LocaleDataervice(loggingService, localeDataFileSystemService);

            // Assert
            expect(service).toBeDefined();
        });
    });

    describe('loadLocaleDataAsync', () => {
        it('should throw an Error if loadLocaleDataAsync() not called.', (done) => {
            // Arrange
            const rootDir = __dirname;
            let localeDataFileSystemService = new LocaleDataFileSystemService(loggingService, rootDir);
            const localeDataKey = 'CMD_01';

            // Act
            const service = new LocaleDataervice(loggingService, localeDataFileSystemService);
            const fct = () => service.getLocalData(localeDataKey);

            // Assert
            try { fct(); }
            catch (e) {
                expect(e).toBeInstanceOf(LocaleDataError);
                const localeDataError = e as LocaleDataError;
                expect(localeDataError.message).toContain(`Locale data type not yet specified !`);
                done();
            };
        });

        it('should throw an Error if loadLocaleDataAsync() called with invalid locale data type.', (done) => {
            // Arrange
            const rootDir = __dirname;
            let localeDataFileSystemService = new LocaleDataFileSystemService(loggingService, rootDir);
            const localeDataType = 'INVALID';
            const localeId = 'en';

            // Act
            const service = new LocaleDataervice(loggingService, localeDataFileSystemService);
            const promiseFct = service.loadLocaleDataAsync(localeDataType, localeId);

            // Assert
            promiseFct.catch((e: Error) => {
                expect(e).toBeInstanceOf(LocaleDataError);
                const localeDataError = e as LocaleDataError;
                expect(localeDataError.message).toContain(`Locale Data cannot be loaded due to [Error: ENOENT: no such file or directory`);
                expect(localeDataError.message).toContain(localeDataType.toLowerCase());
                expect(localeDataError.message).toContain(localeId);
                expect(localeDataError.message).toContain(rootDir);
                done();
            });
        });

        it('should throw an Error if loadLocaleDataAsync() called with invalid locale Id.', (done) => {
            // Arrange
            const rootDir = __dirname;
            let localeDataFileSystemService = new LocaleDataFileSystemService(loggingService, rootDir);
            const localeDataType = 'command-error';
            const localeId = "xx";

            // Act
            const service = new LocaleDataervice(loggingService, localeDataFileSystemService);
            const promiseFct = service.loadLocaleDataAsync(localeDataType, localeId);

            // Assert
            promiseFct.catch((e: Error) => {
                expect(e).toBeInstanceOf(LocaleDataError);
                const localeDataError = e as LocaleDataError;
                expect(localeDataError.message).toContain(`Locale Data cannot be loaded due to [Error: ENOENT: no such file or directory`);
                expect(localeDataError.message).toContain(localeDataType.toLowerCase());
                expect(localeDataError.message).toContain(localeId);
                expect(localeDataError.message).toContain(rootDir);
                done();
            });
        });
    });

    describe('getLocalData', () => {
        it('should get the locale data with default locale id (en).', async () => {
            // Arrange
            const rootDir = __dirname;
            let localeDataFileSystemService = new LocaleDataFileSystemService(loggingService, rootDir);
            const localeDataType = 'command-error';
            const localeDataKey = 'CMD_01';
            const expectedLocaleDataDescription = 'CMD_1 EN Description';

            // Act
            const service = new LocaleDataervice(loggingService, localeDataFileSystemService);
            await service.loadLocaleDataAsync(localeDataType);
            const localeData = service.getLocalData(localeDataKey);

            // Assert
            expect(localeData).toBeDefined();
            expect(localeData?.id).toBe(localeDataKey);
            expect(localeData?.description).toBe(expectedLocaleDataDescription);
        });

        it('should get the locale data with default locale id (fr).', async () => {
            // Arrange
            const rootDir = __dirname;
            let localeDataFileSystemService = new LocaleDataFileSystemService(loggingService, rootDir);
            const localeDataType = 'command-error';
            const localeDataKey = 'CMD_01';
            const localeId = 'fr';
            const expectedLocaleDataDescription = 'CMD_1 FR Description';

            // Act
            const service = new LocaleDataervice(loggingService, localeDataFileSystemService);
            await service.loadLocaleDataAsync(localeDataType, localeId);
            const localeData = service.getLocalData(localeDataKey);

            // Assert
            expect(localeData).toBeDefined();
            expect(localeData?.id).toBe(localeDataKey);
            expect(localeData?.description).toBe(expectedLocaleDataDescription);
        });

        it('should throw an error when getting a locale data with invalid key (not fallback description).', async () => {
            // Arrange
            const rootDir = __dirname;
            let localeDataFileSystemService = new LocaleDataFileSystemService(loggingService, rootDir);
            const localeDataType = 'command-error';
            const localeDataKey = 'INVALID';
            const localeId = 'fr';

            // Act
            const service = new LocaleDataervice(loggingService, localeDataFileSystemService);
            await service.loadLocaleDataAsync(localeDataType, localeId);
            const fct = () => service.getLocalData(localeDataKey);

            // Assert
            try { fct(); }
            catch (e) {
                expect(e).toBeInstanceOf(LocaleDataError);
                const localeDataError = e as LocaleDataError;
                expect(localeDataError.message).toContain(`Local key [${localeDataKey}] not found`);
                expect(localeDataError.message).toContain(localeDataType);
                expect(localeDataError.message).toContain(localeId);
            };
        });

        it('should not throw an error when getting a locale data with invalid key (use a fallback description) nut write warn log.', async (done) => {
            // Arrange
            const rootDir = __dirname;
            let localeDataFileSystemService = new LocaleDataFileSystemService(loggingService, rootDir);
            const localeDataType = 'command-error';
            const localeDataKey = 'INVALID';
            const localeId = 'fr';

            loggingService.warn = jest.fn((msg: LogMessage, ...args: any[]) => {
                expect(msg()).toContain(`Local key [${localeDataKey}] not found`);
                const fallbackLocaleData: LocaleData = {
                    id: localeDataKey,
                    description: LocaleDataervice.FALL_BACK_DESCRIPTION
                }
                expect(args[0]).toEqual(fallbackLocaleData);
                done();
            });

            // Act
            const service = new LocaleDataervice(loggingService, localeDataFileSystemService);
            await service.loadLocaleDataAsync(localeDataType, localeId);
            const localeData = service.getLocalData(localeDataKey);

            // Assert
            expect(localeData).toBeDefined();
            expect(localeData?.id).toBe(localeDataKey);
            expect(localeData?.description).toBe(LocaleDataervice.FALL_BACK_DESCRIPTION);
        });

        it('should throw an error when getting a locale data without loading data.', () => {
            // Arrange
            const rootDir = __dirname;
            let localeDataFileSystemService = new LocaleDataFileSystemService(loggingService, rootDir);
            const localeDataKey = 'INVALID';

            // Act
            const service = new LocaleDataervice(loggingService, localeDataFileSystemService);
            const fct = () => service.getLocalData(localeDataKey);

            // Assert
            try { fct(); }
            catch (e) {
                expect(e).toBeInstanceOf(LocaleDataError);
                const localeDataError = e as LocaleDataError;
                expect(localeDataError.message).toContain(`Locale data type not yet specified !`);
            };
        });
    });
});
