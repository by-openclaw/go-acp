import * as _ from 'lodash';
import { SourceNamesResponseCommand } from '../../../../src/command/tx/106-source-names-response-message/command';
import { SourceNamesResponseCommandParams } from '../../../../src/command/tx/106-source-names-response-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import {
    SourceNamesResponseCommandOptions
} from '../../../../src/command/tx/106-source-names-response-message/options';
import { NameLength } from '../../../../src/command/shared/name-length';

describe('Source Names Response Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: SourceNamesResponseCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstSourceId: 0,
                numberOfSourceNamesToFollow: 32,
                sourceNameItems: Fixture.buildCharItems(64, 4)
            };
            const options: SourceNamesResponseCommandOptions = {
                lengthOfSourceNamesReturned: NameLength.FOUR_CHAR_NAMES
            };
            // Act
            const command = new SourceNamesResponseCommand(params, options);

            // Assert
            expect(command).toBeDefined();
            expect(command.params).toBe(params);
            expect(command.options).toBe(options);
            expect(command.identifier).toBe(CommandIdentifiers.TX.GENERAL.SOURCE_NAMES_RESPONSE_MESSAGE);
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: SourceNamesResponseCommandParams = {
                matrixId: -1,
                levelId: -1,
                firstSourceId: -1,
                numberOfSourceNamesToFollow: 0,
                sourceNameItems: []
            };
            const options: SourceNamesResponseCommandOptions = {
                lengthOfSourceNamesReturned: NameLength.SIXTEEN_CHAR_NAMES
            };
            // Act
            const fct = () => new SourceNamesResponseCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.firstSourceId.id).toBe(
                    CommandErrorsKeys.FIRST_NAME_NUMBER_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceIdAndMaximumNumberOfNames.id).toBe(
                    CommandErrorsKeys.SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceNamesItems.id).toBe(
                    CommandErrorsKeys.SOURCE_NAMES_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: SourceNamesResponseCommandParams = {
                matrixId: 256,
                levelId: 256,
                firstSourceId: 65536,
                numberOfSourceNamesToFollow: 32,
                sourceNameItems: []
            };
            const options: SourceNamesResponseCommandOptions = {
                lengthOfSourceNamesReturned: NameLength.SIXTEEN_CHAR_NAMES
            };
            // Act
            const fct = () => new SourceNamesResponseCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.firstSourceId.id).toBe(
                    CommandErrorsKeys.FIRST_NAME_NUMBER_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceIdAndMaximumNumberOfNames.id).toBe(
                    CommandErrorsKeys.SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceNamesItems.id).toBe(
                    CommandErrorsKeys.SOURCE_NAMES_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General Source Names Response Message CMD_106_0X6a', () => {
            // let commandIdentifier: CommandIdentifier;

            // beforeAll(() => {
            //     commandIdentifier = CommandIdentifiers.TX.GENERAL.SOURCE_NAMES_RESPONSE_MESSAGE;
            // });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SourceNamesResponseCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    firstSourceId: 0,
                    numberOfSourceNamesToFollow: 32,
                    sourceNameItems: Fixture.buildCharItems(64, 4)
                };
                const options: SourceNamesResponseCommandOptions = {
                    lengthOfSourceNamesReturned: NameLength.FOUR_CHAR_NAMES
                };
                let commandIdentifier: CommandIdentifier;
                commandIdentifier = CommandIdentifiers.TX.GENERAL.SOURCE_NAMES_RESPONSE_MESSAGE;
                // Act
                const metaCommand = new SourceNamesResponseCommand(params, options);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            '6a 00 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31',
                        bytesCount: 0x86,
                        checksum: 0x44,
                        buffer:
                            '10 02 6a 00 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31 86 44 10 03'
                    },
                    {
                        data:
                            '6a 00 00 00 20 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33',
                        bytesCount: 0x86,
                        checksum: 0xba,
                        buffer:
                            '10 02 6a 00 00 00 20 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33 86 ba 10 03'
                    }
                ]);
            });
            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: SourceNamesResponseCommandParams = {
                    matrixId: 16,
                    levelId: 0,
                    firstSourceId: 0,
                    numberOfSourceNamesToFollow: 32,
                    sourceNameItems: Fixture.buildCharItems(64, 4)
                };
                const options: SourceNamesResponseCommandOptions = {
                    lengthOfSourceNamesReturned: NameLength.FOUR_CHAR_NAMES
                };
                let commandIdentifier: CommandIdentifier;
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.SOURCE_NAMES_RESPONSE_MESSAGE;
                // Act
                const metaCommand = new SourceNamesResponseCommand(params, options);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            'ea 10 00 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31',
                        bytesCount: 0x87,
                        checksum: 0xb3,
                        buffer:
                            '10 02 ea 10 10 00 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31 87 b3 10 03'
                    },
                    {
                        data:
                            'ea 10 00 00 00 20 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33',
                        bytesCount: 0x87,
                        checksum: 0x29,
                        buffer:
                            '10 02 ea 10 10 00 00 00 20 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33 87 29 10 03'
                    }
                ]);
            });
            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: SourceNamesResponseCommandParams = {
                    matrixId: 0,
                    levelId: 16,
                    firstSourceId: 0,
                    numberOfSourceNamesToFollow: 32,
                    sourceNameItems: Fixture.buildCharItems(64, 4)
                };
                const options: SourceNamesResponseCommandOptions = {
                    lengthOfSourceNamesReturned: NameLength.FOUR_CHAR_NAMES
                };
                let commandIdentifier: CommandIdentifier;
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.SOURCE_NAMES_RESPONSE_MESSAGE;
                // Act
                const metaCommand = new SourceNamesResponseCommand(params, options);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            'ea 00 10 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31',
                        bytesCount: 0x87,
                        checksum: 0xb3,
                        buffer:
                            '10 02 ea 00 10 10 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31 87 b3 10 03'
                    },
                    {
                        data:
                            'ea 00 10 00 00 20 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33',
                        bytesCount: 0x87,
                        checksum: 0x29,
                        buffer:
                            '10 02 ea 00 10 10 00 00 20 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33 87 29 10 03'
                    }
                ]);
            });

        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.EXTENDED.ALL_SOURCE_NAMES_REQUEST_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SourceNamesResponseCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    firstSourceId: 16,
                    numberOfSourceNamesToFollow: 16,
                    sourceNameItems: Fixture.buildCharItems(64, 8)
                };
                const options: SourceNamesResponseCommandOptions = {
                    lengthOfSourceNamesReturned: NameLength.EIGHT_CHAR_NAMES
                };
                // Act
                const metaCommand = new SourceNamesResponseCommand(params, options);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            'ea 10 10 01 00 10 10 30 30 30 30 30 30 30 30 30 30 30 30 30 30 30 31 30 30 30 30 30 30 30 32 30 30 30 30 30 30 30 33 30 30 30 30 30 30 30 34 30 30 30 30 30 30 30 35 30 30 30 30 30 30 30 36 30 30 30 30 30 30 30 37 30 30 30 30 30 30 30 38 30 30 30 30 30 30 30 39 30 30 30 30 30 30 31 30 30 30 30 30 30 30 31 31 30 30 30 30 30 30 31 32 30 30 30 30 30 30 31 33 30 30 30 30 30 30 31 34 30 30 30 30 30 30 31 35',
                        bytesCount: 0x87,
                        checksum: 0x0c,
                        buffer:
                            '10 02 ea 10 10 10 10 01 00 10 10 10 10 30 30 30 30 30 30 30 30 30 30 30 30 30 30 30 31 30 30 30 30 30 30 30 32 30 30 30 30 30 30 30 33 30 30 30 30 30 30 30 34 30 30 30 30 30 30 30 35 30 30 30 30 30 30 30 36 30 30 30 30 30 30 30 37 30 30 30 30 30 30 30 38 30 30 30 30 30 30 30 39 30 30 30 30 30 30 31 30 30 30 30 30 30 30 31 31 30 30 30 30 30 30 31 32 30 30 30 30 30 30 31 33 30 30 30 30 30 30 31 34 30 30 30 30 30 30 31 35 87 0c 10 03'
                    },
                    {
                        data:
                            'ea 10 10 01 00 20 10 30 30 30 30 30 30 31 36 30 30 30 30 30 30 31 37 30 30 30 30 30 30 31 38 30 30 30 30 30 30 31 39 30 30 30 30 30 30 32 30 30 30 30 30 30 30 32 31 30 30 30 30 30 30 32 32 30 30 30 30 30 30 32 33 30 30 30 30 30 30 32 34 30 30 30 30 30 30 32 35 30 30 30 30 30 30 32 36 30 30 30 30 30 30 32 37 30 30 30 30 30 30 32 38 30 30 30 30 30 30 32 39 30 30 30 30 30 30 33 30 30 30 30 30 30 30 33 31',
                        bytesCount: 0x87,
                        checksum: 0xd4,
                        buffer:
                            '10 02 ea 10 10 10 10 01 00 20 10 10 30 30 30 30 30 30 31 36 30 30 30 30 30 30 31 37 30 30 30 30 30 30 31 38 30 30 30 30 30 30 31 39 30 30 30 30 30 30 32 30 30 30 30 30 30 30 32 31 30 30 30 30 30 30 32 32 30 30 30 30 30 30 32 33 30 30 30 30 30 30 32 34 30 30 30 30 30 30 32 35 30 30 30 30 30 30 32 36 30 30 30 30 30 30 32 37 30 30 30 30 30 30 32 38 30 30 30 30 30 30 32 39 30 30 30 30 30 30 33 30 30 30 30 30 30 30 33 31 87 d4 10 03'
                    },
                    {
                        data:
                            'ea 10 10 01 00 40 10 30 30 30 30 30 30 33 32 30 30 30 30 30 30 33 33 30 30 30 30 30 30 33 34 30 30 30 30 30 30 33 35 30 30 30 30 30 30 33 36 30 30 30 30 30 30 33 37 30 30 30 30 30 30 33 38 30 30 30 30 30 30 33 39 30 30 30 30 30 30 34 30 30 30 30 30 30 30 34 31 30 30 30 30 30 30 34 32 30 30 30 30 30 30 34 33 30 30 30 30 30 30 34 34 30 30 30 30 30 30 34 35 30 30 30 30 30 30 34 36 30 30 30 30 30 30 34 37',
                        bytesCount: 0x87,
                        checksum: 0x9e,
                        buffer:
                            '10 02 ea 10 10 10 10 01 00 40 10 10 30 30 30 30 30 30 33 32 30 30 30 30 30 30 33 33 30 30 30 30 30 30 33 34 30 30 30 30 30 30 33 35 30 30 30 30 30 30 33 36 30 30 30 30 30 30 33 37 30 30 30 30 30 30 33 38 30 30 30 30 30 30 33 39 30 30 30 30 30 30 34 30 30 30 30 30 30 30 34 31 30 30 30 30 30 30 34 32 30 30 30 30 30 30 34 33 30 30 30 30 30 30 34 34 30 30 30 30 30 30 34 35 30 30 30 30 30 30 34 36 30 30 30 30 30 30 34 37 87 9e 10 03'
                    },
                    {
                        data:
                            'ea 10 10 01 00 70 10 30 30 30 30 30 30 34 38 30 30 30 30 30 30 34 39 30 30 30 30 30 30 35 30 30 30 30 30 30 30 35 31 30 30 30 30 30 30 35 32 30 30 30 30 30 30 35 33 30 30 30 30 30 30 35 34 30 30 30 30 30 30 35 35 30 30 30 30 30 30 35 36 30 30 30 30 30 30 35 37 30 30 30 30 30 30 35 38 30 30 30 30 30 30 35 39 30 30 30 30 30 30 36 30 30 30 30 30 30 30 36 31 30 30 30 30 30 30 36 32 30 30 30 30 30 30 36 33',
                        bytesCount: 0x87,
                        checksum: 0x58,
                        buffer:
                            '10 02 ea 10 10 10 10 01 00 70 10 10 30 30 30 30 30 30 34 38 30 30 30 30 30 30 34 39 30 30 30 30 30 30 35 30 30 30 30 30 30 30 35 31 30 30 30 30 30 30 35 32 30 30 30 30 30 30 35 33 30 30 30 30 30 30 35 34 30 30 30 30 30 30 35 35 30 30 30 30 30 30 35 36 30 30 30 30 30 30 35 37 30 30 30 30 30 30 35 38 30 30 30 30 30 30 35 39 30 30 30 30 30 30 36 30 30 30 30 30 30 30 36 31 30 30 30 30 30 30 36 32 30 30 30 30 30 30 36 33 87 58 10 03'
                    }
                ]);
            });
        });
    });

    describe('to Log Description of the general command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: SourceNamesResponseCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstSourceId: 0,
                numberOfSourceNamesToFollow: 32,
                sourceNameItems: Fixture.buildCharItems(64, 8)
            };
            const options: SourceNamesResponseCommandOptions = {
                lengthOfSourceNamesReturned: NameLength.EIGHT_CHAR_NAMES
            };
            // Act
            const metaCommand = new SourceNamesResponseCommand(params, options);
            const description = metaCommand.toLogDescription();
            console.log(description);

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
